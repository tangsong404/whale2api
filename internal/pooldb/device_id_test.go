package pooldb

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func newDeviceTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Connect(context.Background(), filepath.Join(t.TempDir(), "device.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func tableColumns(t *testing.T, db *DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.sql.QueryContext(context.Background(), `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMigrationsAddDeviceIDColumnsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migrate.db")
	ctx := context.Background()

	db, err := Connect(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// Reconnect: every migration file runs again and must be ignorable.
	db.Close()
	db, err = Connect(ctx, path)
	if err != nil {
		t.Fatalf("second connect (migration idempotency) failed: %v", err)
	}
	defer db.Close()

	columns := tableColumns(t, db, "pool_accounts")
	for _, want := range []string{"device_id", "device_id_updated_at"} {
		if !columns[want] {
			t.Fatalf("missing column %q; have %v", want, columns)
		}
	}
}

func TestAccountDeviceIDUpsertUpdateClear(t *testing.T) {
	ctx := context.Background()
	db := newDeviceTestDB(t)
	if err := db.CreateGatewayKey(ctx, "sk-1", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAccountToPool(ctx, "sk-1", "a@example.com", "pwd", "B-token-1"); err != nil {
		t.Fatal(err)
	}
	assertStoredDeviceID := func(want string) {
		t.Helper()
		accounts, err := db.LoadAccountsForAPIKey(ctx, "sk-1")
		if err != nil {
			t.Fatal(err)
		}
		if len(accounts) != 1 {
			t.Fatalf("expected 1 account, got %d", len(accounts))
		}
		if accounts[0].DeviceID != want {
			t.Fatalf("LoadAccountsForAPIKey device_id = %q, want %q", accounts[0].DeviceID, want)
		}
		cred, err := db.GetPoolAccountCredential(ctx, "sk-1", "a@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if cred.DeviceID != want {
			t.Fatalf("credential device_id = %q, want %q", cred.DeviceID, want)
		}
	}
	assertStoredDeviceID("B-token-1")

	// Re-adding without a device id keeps the stored one (and the password).
	if err := db.AddAccountToPool(ctx, "sk-1", "a@example.com", "", ""); err != nil {
		t.Fatal(err)
	}
	assertStoredDeviceID("B-token-1")

	if err := db.UpdateAccountDeviceID(ctx, "a@example.com", "B-token-2"); err != nil {
		t.Fatal(err)
	}
	assertStoredDeviceID("B-token-2")

	var deviceID string
	var updatedAt sql.NullString
	if err := db.sql.QueryRowContext(ctx, `SELECT device_id, device_id_updated_at FROM pool_accounts WHERE identifier = ?`, "a@example.com").Scan(&deviceID, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if deviceID != "B-token-2" {
		t.Fatalf("raw device_id = %q", deviceID)
	}
	if !updatedAt.Valid || strings.TrimSpace(updatedAt.String) == "" {
		t.Fatalf("expected device_id_updated_at to be set, got %+v", updatedAt)
	}

	if err := db.ClearAccountDeviceID(ctx, "a@example.com"); err != nil {
		t.Fatal(err)
	}
	assertStoredDeviceID("")
	if err := db.sql.QueryRowContext(ctx, `SELECT device_id_updated_at FROM pool_accounts WHERE identifier = ?`, "a@example.com").Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if updatedAt.Valid {
		t.Fatalf("expected device_id_updated_at to be NULL after clear, got %q", updatedAt.String)
	}
}

func TestUpdateAccountDeviceIDUnknownAccount(t *testing.T) {
	ctx := context.Background()
	db := newDeviceTestDB(t)
	if err := db.UpdateAccountDeviceID(ctx, "missing@example.com", "B-token"); err == nil {
		t.Fatal("expected error for unknown account")
	}
}

func TestAccountRowExposesDeviceStatus(t *testing.T) {
	ctx := context.Background()
	db := newDeviceTestDB(t)
	if err := db.CreateGatewayKey(ctx, "sk-1", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAccountToPool(ctx, "sk-1", "a@example.com", "pwd", "B-abcdefghijklmnop"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ListPoolAccountsAll(ctx, "sk-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if !rows[0].HasDeviceID {
		t.Fatal("expected HasDeviceID")
	}
	if rows[0].DeviceIDPreview == "" || strings.Contains(rows[0].DeviceIDPreview, "abcdefghijklmnop") {
		t.Fatalf("device preview must be masked, got %q", rows[0].DeviceIDPreview)
	}
	if rows[0].DeviceIDUpdatedAt.IsZero() {
		t.Fatal("expected DeviceIDUpdatedAt")
	}
}

func TestImportExportCSVDeviceIDRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := newDeviceTestDB(t)
	if err := db.CreateGatewayKey(ctx, "sk-1", "", ""); err != nil {
		t.Fatal(err)
	}

	// New three-column format with a header.
	res, err := db.ImportAccountsCSV(ctx, "sk-1", strings.NewReader("email,password,device_id\nb@example.com,pw2,B-dev-2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 {
		t.Fatalf("expected 1 imported, got %+v", res)
	}
	// Legacy two-column format without a header.
	res, err = db.ImportAccountsCSV(ctx, "sk-1", strings.NewReader("a@example.com,pw1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 {
		t.Fatalf("expected 1 imported, got %+v", res)
	}
	// Headerless three-column format: third column is the device id.
	res, err = db.ImportAccountsCSV(ctx, "sk-1", strings.NewReader("c@example.com,pw3,B-dev-3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 {
		t.Fatalf("expected 1 imported, got %+v", res)
	}

	body, err := db.ExportAccountsCSV(ctx, "sk-1", true, true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "device_id") {
		t.Fatalf("expected device_id header, got %q", text)
	}
	for _, want := range []string{"B-dev-2", "B-dev-3"} {
		if !strings.Contains(text, want) {
			t.Fatalf("export missing %q: %q", want, text)
		}
	}

	// Round trip the export into a second key.
	if err := db.CreateGatewayKey(ctx, "sk-2", "", ""); err != nil {
		t.Fatal(err)
	}
	res, err = db.ImportAccountsCSV(ctx, "sk-2", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 3 {
		t.Fatalf("expected 3 imported in round trip, got %+v", res)
	}
	accounts, err := db.LoadAccountsForAPIKey(ctx, "sk-2")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, acc := range accounts {
		got[acc.Email] = acc.DeviceID
	}
	if got["a@example.com"] != "" {
		t.Fatalf("legacy account should have no device id, got %q", got["a@example.com"])
	}
	if got["b@example.com"] != "B-dev-2" || got["c@example.com"] != "B-dev-3" {
		t.Fatalf("round-trip device ids lost: %+v", got)
	}

	// Export without the optional column stays on the legacy layout.
	plain, err := db.ExportAccountsCSV(ctx, "sk-1", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "device_id") {
		t.Fatalf("unexpected device_id column: %q", plain)
	}
	if !strings.Contains(string(plain), "email,password,discarded") {
		t.Fatalf("unexpected legacy header: %q", plain)
	}
}

func TestImportHeaderlessLegacyDiscardedColumnNotTreatedAsDeviceID(t *testing.T) {
	ctx := context.Background()
	db := newDeviceTestDB(t)
	if err := db.CreateGatewayKey(ctx, "sk-1", "", ""); err != nil {
		t.Fatal(err)
	}

	// Legacy headerless email,password,discarded rows; the boolean third column
	// must never be imported as a device token.
	res, err := db.ImportAccountsCSV(ctx, "sk-1", strings.NewReader(
		"legacy-true@example.com,pw1,true\n"+
			"legacy-false@example.com,pw2,FALSE\n"+
			"real-device@example.com,pw3,B-real-token\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 3 {
		t.Fatalf("imported = %d, want 3 (%+v)", res.Imported, res)
	}

	accounts, err := db.LoadAccountsForAPIKey(ctx, "sk-1")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, acc := range accounts {
		got[acc.Email] = acc.DeviceID
	}
	if got["legacy-true@example.com"] != "" || got["legacy-false@example.com"] != "" {
		t.Fatalf("legacy discarded column imported as device_id: %+v", got)
	}
	if got["real-device@example.com"] != "B-real-token" {
		t.Fatalf("real device token lost: %+v", got)
	}
}

func TestImportDeviceIDsAliasKeepsFirstToken(t *testing.T) {
	ctx := context.Background()
	db := newDeviceTestDB(t)
	if err := db.CreateGatewayKey(ctx, "sk-1", "", ""); err != nil {
		t.Fatal(err)
	}
	// `device_ids` is the header the harvest CLI historically emitted; multiple
	// tokens are joined with "|". Import must map it and keep the first token.
	res, err := db.ImportAccountsCSV(ctx, "sk-1", strings.NewReader(
		"email,password,device_ids\na@example.com,pw1,B-first|B-second\n"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 {
		t.Fatalf("imported = %d, want 1 (%+v)", res.Imported, res)
	}
	accounts, err := db.LoadAccountsForAPIKey(ctx, "sk-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0].DeviceID != "B-first" {
		t.Fatalf("device_id = %q, want first token B-first", accounts[0].DeviceID)
	}
}

func TestLoadAccountsForAPIKeyKeepsRawMobileIdentifier(t *testing.T) {
	ctx := context.Background()
	db := newDeviceTestDB(t)
	if err := db.CreateGatewayKey(ctx, "sk-1", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAccountToPool(ctx, "sk-1", "13800138000", "pwd", ""); err != nil {
		t.Fatal(err)
	}
	accounts, err := db.LoadAccountsForAPIKey(ctx, "sk-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0].PoolIdentifier != "13800138000" {
		t.Fatalf("PoolIdentifier = %q, want raw mobile", accounts[0].PoolIdentifier)
	}
	if got := accounts[0].Identifier(); got != "13800138000" {
		t.Fatalf("Identifier() = %q, want raw mobile", got)
	}
}
