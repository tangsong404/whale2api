-- Persist mute unblock time for pool accounts.
ALTER TABLE pool_accounts ADD COLUMN mute_until TEXT NOT NULL DEFAULT '';
