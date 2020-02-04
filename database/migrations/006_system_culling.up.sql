ALTER TABLE system_platform
    ADD COLUMN IF NOT EXISTS stale_timestamp TIMESTAMP WITH TIME ZONE;
ALTER TABLE system_platform
    ADD COLUMN IF NOT EXISTS stale_warning_timestamp TIMESTAMP WITH TIME ZONE;
ALTER TABLE system_platform
    ADD COLUMN IF NOT EXISTS culled_timestamp TIMESTAMP WITH TIME ZONE;
ALTER TABLE system_platform
    ADD COLUMN IF NOT EXISTS stale BOOLEAN NOT NULL DEFAULT FALSE;


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

