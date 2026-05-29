-- Persisted account test jobs for Pool UI (survives page refresh).

CREATE TABLE IF NOT EXISTS pool_account_test_jobs (
    api_key TEXT PRIMARY KEY REFERENCES gateway_api_keys (api_key) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'idle',
    total INTEGER NOT NULL DEFAULT 0,
    done INTEGER NOT NULL DEFAULT 0,
    ok_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    results TEXT NOT NULL DEFAULT '[]',
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
