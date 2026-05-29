package pooldb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"whale2api/internal/config"
)

// DB is the SQLite-backed gateway key → account pool store.
type DB struct {
	sql *sql.DB
}

// DefaultDatabasePath is used when WHALE2API_DATABASE_PATH is unset (local dev).
const DefaultDatabasePath = "docker-data/whale2api/whale2api.db"

func Connect(ctx context.Context, path string) (*DB, error) {
	path, err := resolveDatabasePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	dsn := sqliteDSN(path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(2)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	db := &DB{sql: sqlDB}
	if err := migrate(ctx, sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func ConnectFromEnv(ctx context.Context) (*DB, error) {
	path := databasePathFromEnv()
	if path == "" {
		return nil, nil
	}
	return Connect(ctx, path)
}

// DatabasePath returns the configured SQLite file path (for logging).
func DatabasePath() string {
	return databasePathFromEnv()
}

func databasePathFromEnv() string {
	if p := strings.TrimSpace(os.Getenv("WHALE2API_DATABASE_PATH")); p != "" {
		return p
	}
	return DefaultDatabasePath
}

func resolveDatabasePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty database path")
	}
	if !filepath.IsAbs(path) {
		if wd, err := os.Getwd(); err == nil {
			path = filepath.Join(wd, path)
		}
	}
	return filepath.Clean(path), nil
}

func sqliteDSN(path string) string {
	// WAL + busy_timeout help whale2api and poolui share one file.
	return "file:" + filepath.ToSlash(path) + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
}

func (db *DB) Close() {
	if db == nil || db.sql == nil {
		return
	}
	_ = db.sql.Close()
	db.sql = nil
}

func (db *DB) configured() error {
	if db == nil || db.sql == nil {
		return fmt.Errorf("pool db is not configured")
	}
	return nil
}

func maxAccountsPerKey() int {
	raw := strings.TrimSpace(os.Getenv("WHALE2API_POOL_MAX_ACCOUNTS_PER_KEY"))
	if raw == "" || raw == "0" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func limitAccounts(accounts []config.Account) []config.Account {
	max := maxAccountsPerKey()
	if max <= 0 || len(accounts) <= max {
		return accounts
	}
	return accounts[:max]
}
