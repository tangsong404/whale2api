-- Gateway API keys and per-key DeepSeek account pools (SQLite).

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS gateway_api_keys (
    api_key TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    remark TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS pool_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    identifier TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL DEFAULT '',
    discarded INTEGER NOT NULL DEFAULT 0,
    discard_reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS pool_bindings (
    api_key TEXT NOT NULL REFERENCES gateway_api_keys (api_key) ON DELETE CASCADE,
    account_id INTEGER NOT NULL REFERENCES pool_accounts (id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (api_key, account_id)
);

CREATE INDEX IF NOT EXISTS idx_pool_bindings_api_key_position
    ON pool_bindings (api_key, position);
