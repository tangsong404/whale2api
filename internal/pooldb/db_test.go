package pooldb

import (
	"context"
	"path/filepath"
	"testing"
)

func TestConnectSQLiteMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Connect(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	exists, err := db.GatewayKeyExists(context.Background(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected missing key")
	}
}

func TestIsUniqueViolationSQLite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "uniq.db")
	db, err := Connect(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.CreateGatewayKey(ctx, "sk-dup", "a", ""); err != nil {
		t.Fatal(err)
	}
	err = db.CreateGatewayKey(ctx, "sk-dup", "b", "")
	if !IsUniqueViolation(err) {
		t.Fatalf("expected unique violation, got %v", err)
	}
}
