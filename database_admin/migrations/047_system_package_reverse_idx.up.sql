CREATE INDEX IF NOT EXISTS system_package_pkg_system_idx
    ON system_package (rh_account_id, package_id, system_id)
    INCLUDE (latest_evra);
