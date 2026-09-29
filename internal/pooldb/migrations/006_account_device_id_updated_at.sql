-- Timestamp of the last successful device token harvest for an account.
ALTER TABLE pool_accounts ADD COLUMN device_id_updated_at TEXT;
