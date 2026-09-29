-- Per-account Shumei device token used by DeepSeek login risk control.
-- Single statement per file: the migration runner re-executes files on every
-- startup and only tolerates "duplicate column name" errors per file, so a
-- multi-statement file could never recover from a partially applied state.
ALTER TABLE pool_accounts ADD COLUMN device_id TEXT NOT NULL DEFAULT '';
