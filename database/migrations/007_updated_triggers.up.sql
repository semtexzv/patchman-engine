DROP TRIGGER IF EXISTS system_platform_opt_out_cache ON system_platform;
DROP FUNCTION IF EXISTS opt_out_system_update_cache CASCADE;

DROP FUNCTION IF EXISTS refresh_account_cached_counts;
DROP FUNCTION IF EXISTS refresh_advisory_account_cached_counts;
DROP FUNCTION IF EXISTS refresh_advisory_cached_counts;
DROP FUNCTION IF EXISTS refresh_all_cached_counts;
DROP FUNCTION IF EXISTS refresh_system_cached_counts;
DROP FUNCTION IF EXISTS update_system_caches;

-- count system advisories according to advisory type
CREATE OR REPLACE FUNCTION system_advisories_count(system_id_in INT, advisory_type_id_in INT DEFAULT NULL)
    RETURNS INT AS
$system_advisories_count$
DECLARE
    result_cnt INT;
BEGIN
    SELECT COUNT(advisory_id)
    FROM system_advisories sa
             JOIN advisory_metadata am ON sa.advisory_id = am.id
    WHERE (am.advisory_type_id = advisory_type_id_in OR advisory_type_id_in IS NULL)
      AND sa.system_id = system_id_in
      AND sa.when_patched IS NULL
    INTO result_cnt;
    RETURN result_cnt;
END;
$system_advisories_count$ LANGUAGE 'plpgsql';

-- refresh account-advisory caches
CREATE OR REPLACE FUNCTION refresh_advisory_caches(account_id_in INT DEFAULT NULL,
                                                   advisory_id_in INT DEFAULT NULL)
    RETURNS INTEGER
AS
$refresh_advisory$
DECLARE
    res INTEGER;
BEGIN

    WITH affected as (
        SELECT am.id            as advisory_id,
               sp.rh_account_id as rh_account_id,
               count(sp.id)     as systems_affected
        FROM advisory_metadata am
                 JOIN system_advisories sa on sa.advisory_id = am.id
                 JOIN system_platform sp on sp.id = sa.system_id
        WHERE (sp.rh_account_id = account_id_in OR account_id_in IS NULL)
          AND (am.id = advisory_id_in OR advisory_id_in IS NULL)
          AND sp.opt_out = false
          AND sp.stale = false
          AND sp.last_evaluation IS NOT NULL
          AND sa.when_patched IS NULL
        GROUP BY am.id, sp.rh_account_id
    ),
         tmp as (
             DELETE FROM advisory_account_data aad
                 WHERE (aad.rh_account_id = account_id_in OR account_id_in IS NULL)
                     AND (aad.advisory_id = advisory_id_in OR advisory_id_in IS NULL)
                     AND ((aad.rh_account_id, aad.advisory_id) NOT IN (select rh_account_id, advisory_id from affected))
         ),
         upd as (
             INSERT INTO advisory_account_data (advisory_id, rh_account_id, systems_affected)
                 SELECT advisory_id, rh_account_id, systems_affected
                 FROM affected a
                 ON CONFLICT (advisory_id, rh_account_id)
                     DO UPDATE SET systems_affected = excluded.systems_affected
         )
    SELECT count(*)
    FROM affected
    INTO RES;
    RETURN RES;
END;
$refresh_advisory$ language plpgsql;



CREATE OR REPLACE FUNCTION refresh_system_caches(account_id_in INT DEFAULT NULL,
                                                 system_id_in INT DEFAULT NULL)
    RETURNS VOID
AS
$refresh_system$
BEGIN
    -- update advisory count for system, or all systems for per account
    UPDATE system_platform
    SET advisory_count_cache     = system_advisories_count(sp.id, NULL),
        advisory_enh_count_cache = system_advisories_count(sp.id, 1),
        advisory_bug_count_cache = system_advisories_count(sp.id, 2),
        advisory_sec_count_cache = system_advisories_count(sp.id, 3)
    FROM system_platform sp
    WHERE (sp.id = system_id_in or system_id_in IS NULL)
      AND (sp.rh_account_id = account_id_in OR account_id_in IS NULL);
END;
$refresh_system$ language plpgsql;


CREATE OR REPLACE FUNCTION update_system_platform()
    RETURNS TRIGGER AS
$update_platform$
BEGIN
    IF NEW.last_evaluation IS NULL OR tg_op != 'UPDATE' THEN
        -- Not an update
        RETURN NEW;
    END IF;

    IF OLD.opt_out = NEW.opt_out AND OLD.stale = NEW.stale THEN
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
        ON CONFLICT (advisory_id, rh_account_id)
            DO UPDATE SET systems_affected = advisory_account_data.systems_affected + excluded.systems_affected;

    ELSE
        -- Decrement per_account counts
        UPDATE advisory_account_data
        SET systems_affected = aad.systems_affected - 1
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
          AND systems_affected <= 0;
    END IF;

    RETURN NEW;
END;
$update_platform$ LANGUAGE 'plpgsql';


CREATE OR REPLACE FUNCTION update_system_advisory()
    RETURNS TRIGGER AS
$update_system$
DECLARE
    CHANGED RECORD;
    system  RECORD;
BEGIN
    -- Changed can be used to refer to new form of affected row, or the old row in case of deletion
    IF (TG_OP = 'INSERT' OR TG_OP = 'UPDATE') THEN
        CHANGED = NEW;
    ELSIF (TG_OP = 'DELETE') THEN
        CHANGED = OLD;
    END IF;

    SELECT * FROM system_platform where system_platform.id = CHANGED.system_id INTO system;

    -- System was opted out or marked stale, it is not included in counts, We don't have to modify them
    IF system.opt_out OR system.stale THEN
        RETURN CHANGED;
    END IF;

    -- Increment only when inserting as unpatched
    -- or updated from patched to unpatched
    IF (TG_OP = 'INSERT' AND NEW.when_patched IS NULL) OR
       (TG_OP = 'UPDATE' AND OLD.when_patched IS NOT NULL AND NEW.when_patched IS NULL) THEN

        INSERT INTO advisory_account_data(advisory_id, rh_account_id, systems_affected, systems_status_divergent)
        SELECT CHANGED.advisory_id, ra.id, 1, 0
        FROM rh_account ra
        ON CONFLICT (advisory_id, rh_account_id) DO UPDATE
            SET systems_affected = advisory_account_data.systems_affected + excluded.systems_affected;

        -- Delete, as unpatched, decrement counts
        -- Patched, decrement counts
    ELSIF (TG_OP = 'DELETE' AND OLD.when_patched IS NULL) OR
          (TG_OP = 'UPDATE' AND OLD.when_patched IS NULL AND NEW.when_patched IS NOT NULL) THEN

        UPDATE advisory_account_data
        SET systems_affected = aad.systems_affected - 1
        FROM advisory_account_data aad
        WHERE aad.advisory_id = CHANGED.advisory_id
          AND aad.rh_account_id = system.rh_account_id;

        DELETE
        FROM advisory_account_data aad
        WHERE aad.advisory_id = CHANGED.advisory_id
          AND aad.rh_account_id = system.rh_account_id
          AND systems_affected <= 0;

    END IF;
    RETURN CHANGED;
END;
$update_system$ language plpgsql;

CREATE TRIGGER system_platform_on_update
    AFTER UPDATE
    ON system_platform
    FOR EACH ROW
EXECUTE PROCEDURE update_system_platform();

CREATE TRIGGER system_advisories_on_update
    AFTER INSERT OR UPDATE
    ON system_advisories
    FOR EACH ROW
EXECUTE PROCEDURE update_system_advisory();