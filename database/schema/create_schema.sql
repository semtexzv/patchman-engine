CREATE TABLE IF NOT EXISTS schema_migrations
(
    version bigint  NOT NULL,
    dirty   boolean NOT NULL,
    PRIMARY KEY (version)
) TABLESPACE pg_default;


INSERT INTO schema_migrations
VALUES (7, false);

-- ---------------------------------------------------------------------------
-- Functions
-- ---------------------------------------------------------------------------

-- empty
CREATE OR REPLACE FUNCTION empty(t TEXT)
    RETURNS BOOLEAN as
$empty$
BEGIN
    RETURN t ~ '^[[:space:]]*$';
END;
$empty$
    LANGUAGE 'plpgsql';

-- set_first_reported
CREATE OR REPLACE FUNCTION set_first_reported()
    RETURNS TRIGGER AS
$set_first_reported$
BEGIN
    IF NEW.first_reported IS NULL THEN
        NEW.first_reported := CURRENT_TIMESTAMP;
    END IF;
    RETURN NEW;
END;
$set_first_reported$
    LANGUAGE 'plpgsql';

-- set_last_updated
CREATE OR REPLACE FUNCTION set_last_updated()
    RETURNS TRIGGER AS
$set_last_updated$
BEGIN
    IF (TG_OP = 'UPDATE') OR
       NEW.last_updated IS NULL THEN
        NEW.last_updated := CURRENT_TIMESTAMP;
    END IF;
    RETURN NEW;
END;
$set_last_updated$
    LANGUAGE 'plpgsql';

-- check_unchanged
CREATE OR REPLACE FUNCTION check_unchanged()
    RETURNS TRIGGER AS
$check_unchanged$
BEGIN
    IF (TG_OP = 'INSERT') AND
       NEW.unchanged_since IS NULL THEN
        NEW.unchanged_since := CURRENT_TIMESTAMP;
    END IF;
    IF (TG_OP = 'UPDATE') AND
       NEW.json_checksum <> OLD.json_checksum THEN
        NEW.unchanged_since := CURRENT_TIMESTAMP;
    END IF;
    RETURN NEW;
END;
$check_unchanged$
    LANGUAGE 'plpgsql';


CREATE OR REPLACE FUNCTION delete_culled_systems()
    RETURNS INTEGER
AS
$delete_culled$
DECLARE
    culled INTEGER;
BEGIN
    SELECT count(*)
    FROM (
             SELECT delete_system(id)
             FROM system_platform
             WHERE culled_timestamp > now()
         ) t
    INTO culled;
    RETURN culled;
END;
$delete_culled$ language plpgsql;


CREATE OR REPLACE FUNCTION mark_stale_systems()
    RETURNS INTEGER
AS
$mark_stale$
DECLARE
    marked_stale INTEGER;
BEGIN
    WITH updated as (UPDATE system_platform SET stale = true
        WHERE stale_timestamp > now()
            AND stale = false
        RETURNING *)
    select count(*)
    from updated
    INTO marked_stale;
    RETURN marked_stale;
END;
$mark_stale$ language plpgsql;


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
          AND sa.when_patched is null
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
       (tg_op = 'UPDATE' AND OLD.when_patched IS NOT NULL AND NEW.when_patched IS NULL) THEN

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


CREATE OR REPLACE FUNCTION delete_system(inventory_id_in varchar)
    RETURNS TABLE
            (
                deleted_inventory_id TEXT
            )
AS
$delete_system$
BEGIN
    -- opt out to refresh cache and then delete
    WITH locked_row AS (
        SELECT id
        FROM system_platform
        WHERE inventory_id = inventory_id_in
            FOR UPDATE
    )
    UPDATE system_platform
    SET opt_out = true
    WHERE inventory_id = inventory_id_in;
    DELETE
    FROM system_advisories
    WHERE system_id = (SELECT id from system_platform WHERE inventory_id = inventory_id_in);
    DELETE
    FROM system_repo
    WHERE system_id = (SELECT id from system_platform WHERE inventory_id = inventory_id_in);
    RETURN QUERY DELETE FROM system_platform
        WHERE inventory_id = inventory_id_in
        RETURNING inventory_id;
END;
$delete_system$
    LANGUAGE 'plpgsql';

-- rh_account
CREATE TABLE IF NOT EXISTS rh_account
(
    id   INT GENERATED BY DEFAULT AS IDENTITY,
    name TEXT NOT NULL UNIQUE,
    CHECK (NOT empty(name)),
    PRIMARY KEY (id)
) TABLESPACE pg_default;

GRANT SELECT, INSERT, UPDATE, DELETE ON rh_account TO listener;

-- system_platform
CREATE TABLE IF NOT EXISTS system_platform
(
    id                       INT GENERATED BY DEFAULT AS IDENTITY,
    inventory_id             TEXT                     NOT NULL,
    CHECK (NOT empty(inventory_id)),
    rh_account_id            INT                      NOT NULL,
    first_reported           TIMESTAMP WITH TIME ZONE NOT NULL,
    vmaas_json               TEXT,
    json_checksum            TEXT,
    last_updated             TIMESTAMP WITH TIME ZONE NOT NULL,
    unchanged_since          TIMESTAMP WITH TIME ZONE NOT NULL,
    last_evaluation          TIMESTAMP WITH TIME ZONE,
    opt_out                  BOOLEAN                  NOT NULL DEFAULT FALSE,
    advisory_count_cache     INT                      NOT NULL DEFAULT 0,
    advisory_enh_count_cache INT                      NOT NULL DEFAULT 0,
    advisory_bug_count_cache INT                      NOT NULL DEFAULT 0,
    advisory_sec_count_cache INT                      NOT NULL DEFAULT 0,

    last_upload              TIMESTAMP WITH TIME ZONE,
    stale_timestamp          TIMESTAMP WITH TIME ZONE,
    stale_warning_timestamp  TIMESTAMP WITH TIME ZONE,
    culled_timestamp         TIMESTAMP WITH TIME ZONE,
    stale                    BOOLEAN                  NOT NULL DEFAULT FALSE,
    PRIMARY KEY (id),
    UNIQUE (inventory_id),
    CONSTRAINT rh_account_id
        FOREIGN KEY (rh_account_id)
            REFERENCES rh_account (id)
) TABLESPACE pg_default;

CREATE INDEX ON system_platform (rh_account_id);

CREATE TRIGGER system_platform_set_first_reported
    BEFORE INSERT
    ON system_platform
    FOR EACH ROW
EXECUTE PROCEDURE set_first_reported();

CREATE TRIGGER system_platform_set_last_updated
    BEFORE INSERT OR UPDATE
    ON system_platform
    FOR EACH ROW
EXECUTE PROCEDURE set_last_updated();

CREATE TRIGGER system_platform_check_unchanged
    BEFORE INSERT OR UPDATE
    ON system_platform
    FOR EACH ROW
EXECUTE PROCEDURE check_unchanged();

CREATE TRIGGER system_platform_on_update
    AFTER UPDATE
    ON system_platform
    FOR EACH ROW
EXECUTE PROCEDURE update_system_platform();

GRANT SELECT, INSERT, UPDATE, DELETE ON system_platform TO listener;
-- evaluator needs to update last_evaluation
GRANT UPDATE ON system_platform TO evaluator;
-- manager needs to update cache and delete systems
GRANT UPDATE (advisory_count_cache,
              advisory_enh_count_cache,
              advisory_bug_count_cache,
              advisory_sec_count_cache), DELETE ON system_platform TO manager;

-- advisory_type
CREATE TABLE IF NOT EXISTS advisory_type
(
    id   INT  NOT NULL,
    name TEXT NOT NULL UNIQUE,
    CHECK (NOT empty(name)),
    PRIMARY KEY (id)
) TABLESPACE pg_default;

INSERT INTO advisory_type (id, name)
VALUES (0, 'unknown'),
       (1, 'enhancement'),
       (2, 'bugfix'),
       (3, 'security')
ON CONFLICT DO NOTHING;

CREATE TABLE advisory_severity
(
    id   INT  NOT NULL,
    name TEXT NOT NULL UNIQUE CHECK ( not empty(name) ),
    PRIMARY KEY (id)
);

INSERT INTO advisory_severity (id, name)
VALUES (1, 'Low'),
       (2, 'Moderate'),
       (3, 'Important'),
       (4, 'Critical')
ON CONFLICT DO NOTHING;

-- advisory_metadata
CREATE TABLE IF NOT EXISTS advisory_metadata
(
    id               INT GENERATED BY DEFAULT AS IDENTITY,
    name             TEXT                     NOT NULL,
    CHECK (NOT empty(name)),
    description      TEXT                     NOT NULL,
    CHECK (NOT empty(description)),
    synopsis         TEXT                     NOT NULL,
    CHECK (NOT empty(synopsis)),
    summary          TEXT                     NOT NULL,
    CHECK (NOT empty(summary)),
    solution         TEXT                     NOT NULL,
    CHECK (NOT empty(solution)),
    advisory_type_id INT                      NOT NULL,
    public_date      TIMESTAMP WITH TIME ZONE NULL,
    modified_date    TIMESTAMP WITH TIME ZONE NULL,
    url              TEXT,
    severity_id      INT,
    UNIQUE (name),
    PRIMARY KEY (id),
    CONSTRAINT advisory_type_id
        FOREIGN KEY (advisory_type_id)
            REFERENCES advisory_type (id),
    CONSTRAINT advisory_severity_id
        FOREIGN KEY (severity_id)
            REFERENCES advisory_severity (id)
) TABLESPACE pg_default;

CREATE INDEX ON advisory_metadata (advisory_type_id);

GRANT SELECT, INSERT, UPDATE, DELETE ON advisory_metadata TO evaluator;
GRANT SELECT, INSERT, UPDATE, DELETE ON advisory_metadata TO vmaas_sync;
-- TODO: Remove
GRANT SELECT, INSERT, UPDATE, DELETE ON advisory_metadata TO listener;

-- status table
CREATE TABLE IF NOT EXISTS status
(
    id   INT  NOT NULL,
    name TEXT NOT NULL UNIQUE,
    CHECK (NOT empty(name)),
    PRIMARY KEY (id)
) TABLESPACE pg_default;

INSERT INTO status (id, name)
VALUES (0, 'Not Reviewed'),
       (1, 'In-Review'),
       (2, 'On-Hold'),
       (3, 'Scheduled for Patch'),
       (4, 'Resolved'),
       (5, 'No Action')
ON CONFLICT DO NOTHING;


-- system_advisories
CREATE TABLE IF NOT EXISTS system_advisories
(
    id             INT GENERATED BY DEFAULT AS IDENTITY,
    system_id      INT                      NOT NULL,
    advisory_id    INT                      NOT NULL,
    first_reported TIMESTAMP WITH TIME ZONE NOT NULL,
    when_patched   TIMESTAMP WITH TIME ZONE DEFAULT NULL,
    status_id      INT                      DEFAULT 0,
    UNIQUE (system_id, advisory_id),
    PRIMARY KEY (id),
    CONSTRAINT system_platform_id
        FOREIGN KEY (system_id)
            REFERENCES system_platform (id),
    CONSTRAINT advisory_metadata_id
        FOREIGN KEY (advisory_id)
            REFERENCES advisory_metadata (id),
    CONSTRAINT status_id
        FOREIGN KEY (status_id)
            REFERENCES status (id)
) TABLESPACE pg_default;

CREATE INDEX ON system_advisories (status_id);

CREATE TRIGGER system_advisories_set_first_reported
    BEFORE INSERT
    ON system_advisories
    FOR EACH ROW
EXECUTE PROCEDURE set_first_reported();

CREATE TRIGGER system_advisories_on_update
    AFTER INSERT OR UPDATE
    ON system_advisories
    FOR EACH ROW
EXECUTE PROCEDURE update_system_advisory();

GRANT SELECT, INSERT, UPDATE, DELETE ON system_advisories TO evaluator;
-- manager needs to be able to update things like 'status' on a sysid/advisory combination, also needs to delete
GRANT UPDATE, DELETE ON system_advisories TO manager;
-- manager needs to be able to update opt_out column
GRANT UPDATE (opt_out) ON system_platform TO manager;
-- listener deletes systems, TODO: temporary added evaluator permissions to listener
GRANT SELECT, INSERT, UPDATE, DELETE ON system_advisories TO listener;

-- advisory_account_data
CREATE TABLE IF NOT EXISTS advisory_account_data
(
    advisory_id              INT NOT NULL,
    rh_account_id            INT NOT NULL,
    status_id                INT NOT NULL DEFAULT 0,
    systems_affected         INT NOT NULL DEFAULT 0,
    systems_status_divergent INT NOT NULL DEFAULT 0,
    CONSTRAINT advisory_metadata_id
        FOREIGN KEY (advisory_id)
            REFERENCES advisory_metadata (id),
    CONSTRAINT rh_account_id
        FOREIGN KEY (rh_account_id)
            REFERENCES rh_account (id),
    CONSTRAINT status_id
        FOREIGN KEY (status_id)
            REFERENCES status (id),
    UNIQUE (advisory_id, rh_account_id)
) TABLESPACE pg_default;

-- manager needs to write into advisory_account_data table
GRANT SELECT, INSERT, UPDATE, DELETE ON advisory_account_data TO manager;

-- manager user needs to change this table for opt-out functionality
GRANT SELECT, INSERT, UPDATE, DELETE ON advisory_account_data TO manager;
-- evaluator user needs to change this table
GRANT SELECT, INSERT, UPDATE, DELETE ON advisory_account_data TO evaluator;
-- listner user needs to change this table when deleting system
GRANT SELECT, INSERT, UPDATE, DELETE ON advisory_account_data TO listener;

-- repo
CREATE TABLE IF NOT EXISTS repo
(
    id   INT GENERATED BY DEFAULT AS IDENTITY,
    name TEXT NOT NULL UNIQUE,
    CHECK (NOT empty(name)),
    PRIMARY KEY (id)
) TABLESPACE pg_default;

GRANT SELECT, INSERT, UPDATE, DELETE ON repo TO listener;


-- system_repo
CREATE TABLE IF NOT EXISTS system_repo
(
    system_id INT NOT NULL,
    repo_id   INT NOT NULL,
    UNIQUE (system_id, repo_id),
    CONSTRAINT system_platform_id
        FOREIGN KEY (system_id)
            REFERENCES system_platform (id),
    CONSTRAINT repo_id
        FOREIGN KEY (repo_id)
            REFERENCES repo (id)
) TABLESPACE pg_default;

CREATE INDEX ON system_repo (system_id);
CREATE INDEX ON system_repo (repo_id);

GRANT SELECT, INSERT, UPDATE, DELETE ON system_repo TO listener;
GRANT DELETE ON system_repo TO manager;


-- timestamp_kv
CREATE TABLE IF NOT EXISTS timestamp_kv
(
    name  TEXT                     NOT NULL UNIQUE,
    CHECK (NOT empty(name)),
    value TIMESTAMP WITH TIME ZONE NOT NULL
) TABLESPACE pg_default;

GRANT SELECT, INSERT, UPDATE, DELETE ON timestamp_kv TO vmaas_sync;

-- vmaas_sync needs to delete from this tables to sync CVEs correctly
GRANT DELETE ON system_advisories TO vmaas_sync;
GRANT DELETE ON advisory_account_data TO vmaas_sync;

-- ----------------------------------------------------------------------------
-- Read access for all users
-- ----------------------------------------------------------------------------

-- user for evaluator component
GRANT SELECT ON ALL TABLES IN SCHEMA public TO evaluator;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO evaluator;

-- user for listener component
GRANT SELECT ON ALL TABLES IN SCHEMA public TO listener;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO listener;

-- user for UI manager component
GRANT SELECT ON ALL TABLES IN SCHEMA public TO manager;

-- user for VMaaS sync component
GRANT SELECT ON ALL TABLES IN SCHEMA public TO vmaas_sync;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO vmaas_sync;
