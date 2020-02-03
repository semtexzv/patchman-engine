ALTER TABLE system_platform
    ADD COLUMN IF NOT EXISTS stale_timestamp TIMESTAMP WITH TIME ZONE;
ALTER TABLE system_platform
    ADD COLUMN IF NOT EXISTS stale_warning_timestamp TIMESTAMP WITH TIME ZONE;
ALTER TABLE system_platform
    ADD COLUMN IF NOT EXISTS culled_timestamp TIMESTAMP WITH TIME ZONE;

CREATE OR REPLACE FUNCTION delete_culled_systems()
    RETURNS INTEGER
AS
$fun$
DECLARE
    culled integer;
BEGIN
    select count(*)
    from (
             select delete_system(id)
             from system_platform
             where culled_timestamp > now()
         ) t
    INTO culled;
    RETURN culled;
END;
$fun$
    language plpgsql;