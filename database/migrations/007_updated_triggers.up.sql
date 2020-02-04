DROP FUNCTION IF EXISTS opt_out_system_update_cache CASCADE;

DROP FUNCTION IF EXISTS refresh_account_cached_counts;
DROP FUNCTION IF EXISTS refresh_advisory_account_cached_counts;
DROP FUNCTION IF EXISTS refresh_advisory_cached_counts;
DROP FUNCTION IF EXISTS refresh_all_cached_counts;
DROP FUNCTION IF EXISTS refresh_system_cached_counts;
DROP FUNCTION IF EXISTS update_system_caches;

-- Use views as a slow source truth

-- Stores advisory caches on per-account basis
CREATE OR REPLACE VIEW calc_advisory_account_data as
(
select am.id                                as advisory_id,
       sp.rh_account_id                     as rh_account_id,
       count(sp.id) FILTER (
           WHERE sp.opt_out = false
               and sp.stale = false
               and sa.when_patched is null) as systems_affected
FROM advisory_metadata am
         JOIN system_advisories sa on sa.advisory_id = am.id
         JOIN system_platform sp on sp.id = sa.system_id
GROUP BY am.id, sp.rh_account_id);

-- Stores advisory caches on per-system basis
CREATE OR REPLACE VIEW calc_system_platform_counts as
(
SELECT sp.id                             as id,
       sp.rh_account_id                  as rh_account_id,
       system_advisories_count(sp.id, 1) as advisory_enh_count_cache,
       system_advisories_count(sp.id, 2) as advisory_bug_count_cache,
       system_advisories_count(sp.id, 3) as advisory_sec_count_cache
FROM system_platform sp);


CREATE OR REPLACE FUNCTION refresh_cached_counts(account_id_in INT DEFAULT NULL,
                                                 advisory_id_in INT DEFAULT NULL,
                                                 system_id_in INT DEFAULT NULL)
    RETURNS VOID
AS
$refresh$
BEGIN

    -- Update advisory-account pairs, only if system id was not provided, since provided
    -- params serve to constrain the change set
    IF system_id_in IS NULL THEN
        PERFORM (
            WITH affected as (
                SELECT *
                FROM calc_advisory_account_data c
                WHERE (c.rh_account_id = account_id_in or account_id_in IS NULL)
                  AND (c.advisory_id = advisory_id_in OR advisory_id_in IS NULL)
            ),
                 upd as (
                     INSERT INTO advisory_account_data (advisory_id, rh_account_id, systems_affected)
                         SELECT *
                         FROM affected
                         ON CONFLICT (advisory_id, rh_account_id)
                             DO UPDATE SET systems_affected = affected.systems_affected
                                 where affected.systems_affected > 0
                 ),
                 del as (
                     DELETE FROM advisory_account_data aad
                         USING advisory_account_data aad
                             JOIN affected aff on aad.rh_account_id = aff.rh_account_id and
                                                  aad.advisory_id = aff.advisory_id
                         WHERE aff.systems_affected = 0
                 )
            select count(*)
            from affected);
    END IF;

    -- update advisory count for system, or all systems for per account
    IF system_id_in IS NOT NULL OR account_id_in IS NOT NULL THEN
        UPDATE system_platform sp
        SET advisory_count_cache     = (
            SELECT COUNT(advisory_id)
            FROM system_advisories sa
            WHERE sa.system_id = sp.id
              AND sa.when_patched IS NULL
        ),
            advisory_enh_count_cache = system_advisories_count(sp.id, 1),
            advisory_bug_count_cache = system_advisories_count(sp.id, 2),
            advisory_sec_count_cache = system_advisories_count(sp.id, 3)
        WHERE (sp.id = system_id_in or system_id_in is null)
          AND (sp.rh_account_id = account_id_in or account_id_in is null);
    END IF;
END;

$refresh$ language plpgsql;

SELECT *
FROM refresh_cached_counts(account_id_in := 1, system_id_in := 1);


CREATE OR REPLACE FUNCTION update_system_platform()
    RETURNS TRIGGER AS
$opt_out_system_update_cache$
BEGIN
    IF NEW.last_evaluation IS NULL OR tg_op != 'UPDATE' THEN
        -- Not an update
        RETURN NEW;
    END IF;

    IF OLD.opt_out == NEW.opt_out AND OLD.stale == NEW.stale THEN
        -- Nothing changed
        RETURN NEW;
    END IF;

    IF NEW.opt_out = FALSE AND NEW.stale = FALSE THEN

        -- Attempt to insert as 1, on conflict add to existing, smart trick by jdobes
        INSERT INTO advisory_account_data (advisory_id, rh_account_id, systems_affected)
        SELECT sa.advisory_id, NEW.rh_account_id, 1
        FROM system_advisories sa
                 JOIN system_platform sp on sa.system_id = sp.id
        WHERE sa.system_id = NEW.id
          AND sa.when_patched IS NULL
        ON CONFLICT (advisory_id, rh_account_id, systems_affected)
            DO UPDATE SET systems_affected = advisory_account_data.systems_affected + excluded.systems_affected;

    ELSE
        -- Decrement per_account counts
        UPDATE advisory_account_data aad
        SET systems_affected = systems_affected - 1
        FROM advisory_account_data aad
                 JOIN system_advisories sa on aad.advisory_id = sa.advisory_id
                 JOIN system_platform sp on sa.system_id = sp.id
        WHERE sa.system_id = NEW.id
          AND aad.rh_account_id = NEW.rh_account_id
          AND sa.when_patched IS NULL;

        -- Delete 0 entries
        DELETE
        FROM advisory_account_data
        WHERE rh_account_id = NEW.rh_account_id
          AND systems_affected = 0;
    END IF;

    RETURN NEW;
END;
$opt_out_system_update_cache$
    LANGUAGE 'plpgsql';


CREATE TRIGGER system_advisories_counts_on_update_platform
    AFTER UPDATE
    ON system_platform
    FOR EACH ROW
EXECUTE PROCEDURE update_system_platform();



CREATE OR REPLACE FUNCTION update_advisory()
    RETURNS TRIGGER AS
$alter$
DECLARE
    CHANGED RECORD;
BEGIN
    -- Changed can be used to refer to new form of affected row, or the old row in case of deletion
    IF (tg_op == 'INSERT' || tg_op == 'UPDATE') THEN
        CHANGED = NEW;
    ELSIF (tg_op == 'DELETE') THEN
        CHANGED = OLD;
    END IF;

    -- Increment only when inserting as unpatched
    -- or updated from patched to unpatched
    IF (TG_OP == 'INSERT' AND NEW.when_patched IS NULL) OR
       (tg_op == 'UPDATE' AND OLD.when_patched IS NOT NULL AND NEW.when_patched IS NULL) THEN

        INSERT INTO advisory_account_data(advisory_id, rh_account_id, systems_affected, systems_status_divergent)
        SELECT CHANGED.id, ra.id, 1, 0
        FROM rh_account ra
        ON CONFLICT (rh_account_id) DO UPDATE
            SET systems_affected = advisory_account_data.systems_affected + excluded.systems_affected;

        -- Delete, decrement counts
        -- Patched, decrement counts
    ELSIF (TG_OP == 'DELETE') OR
          (TG_OP == 'UPDATE' AND OLD.when_patched IS NULL AND NEW.when_patched IS NOT NULL) THEN

        UPDATE advisory_account_data
        SET systems_affected = systems_affected - 1
        FROM advisory_account_data aad
        where AAD.advisory_id = CHANGED.id;

        DELETE
        FROM advisory_account_data aad
        where aad.advisory_id = CHANGED.id
          AND systems_affected = 0;

    END IF;
END;
$alter$ language plpgsql;


CREATE TRIGGER system_advisories_counts_on_update
    AFTER INSERT OR UPDATE
    ON system_advisories
    FOR EACH ROW
EXECUTE PROCEDURE update_advisory();


CREATE TRIGGER system_advisories_counts_on_delete
    BEFORE DELETE
    ON system_advisories
    FOR EACH ROW
EXECUTE PROCEDURE update_advisory();