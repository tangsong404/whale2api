package poolui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"whale2api/internal/pooldb"
)

func newPoolUITestServer(t *testing.T) (*Server, *pooldb.DB) {
	t.Helper()
	db, err := pooldb.Connect(context.Background(), filepath.Join(t.TempDir(), "poolui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	s, err := NewServer(db)
	if err != nil {
		t.Fatal(err)
	}
	s.Token = "test-token"
	return s, db
}

func doJSON(t *testing.T, s *Server, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Router.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec, out
}

func TestClearAccountDeviceIDHandler(t *testing.T) {
	ctx := context.Background()
	s, db := newPoolUITestServer(t)
	if err := db.CreateGatewayKey(ctx, "sk-test", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAccountToPool(ctx, "sk-test", "a@example.com", "pw", "B-abcdefghijklmnop"); err != nil {
		t.Fatal(err)
	}

	rec, out := doJSON(t, s, http.MethodPost, "/api/keys/sk-test/accounts/device-id/clear", map[string]string{"identifier": "a@example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if out["success"] != true {
		t.Fatalf("unexpected response: %v", out)
	}
	cred, err := db.GetPoolAccountCredential(ctx, "sk-test", "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if cred.DeviceID != "" {
		t.Fatalf("device_id not cleared: %q", cred.DeviceID)
	}

	// Accounts outside the pool are rejected instead of silently mutated.
	rec, _ = doJSON(t, s, http.MethodPost, "/api/keys/sk-test/accounts/device-id/clear", map[string]string{"identifier": "missing@example.com"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown account status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAddAccountWithDeviceIDKeepsFirstPipeToken(t *testing.T) {
	ctx := context.Background()
	s, db := newPoolUITestServer(t)
	if err := db.CreateGatewayKey(ctx, "sk-test", "", ""); err != nil {
		t.Fatal(err)
	}
	rec, _ := doJSON(t, s, http.MethodPost, "/api/keys/sk-test/accounts", map[string]string{
		"email":     "b@example.com",
		"password":  "pw",
		"device_id": "B-abcdefghijklmnop|B-othertoken",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	cred, err := db.GetPoolAccountCredential(ctx, "sk-test", "b@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if cred.DeviceID != "B-abcdefghijklmnop" {
		t.Fatalf("device_id = %q, want first pipe-separated token", cred.DeviceID)
	}

	// Legacy body without device_id stays valid.
	rec, _ = doJSON(t, s, http.MethodPost, "/api/keys/sk-test/accounts", map[string]string{
		"email": "c@example.com", "password": "pw",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy add status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListAccountsExposesDeviceIDStatus(t *testing.T) {
	ctx := context.Background()
	s, db := newPoolUITestServer(t)
	if err := db.CreateGatewayKey(ctx, "sk-test", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.AddAccountToPool(ctx, "sk-test", "a@example.com", "pw", "B-abcdefghijklmnop"); err != nil {
		t.Fatal(err)
	}
	rec, _ := doJSON(t, s, http.MethodGet, "/api/keys/sk-test/accounts", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Accounts []struct {
			HasDeviceID       bool   `json:"has_device_id"`
			DeviceIDPreview   string `json:"device_id_preview"`
			DeviceIDUpdatedAt string `json:"device_id_updated_at"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 || !payload.Accounts[0].HasDeviceID {
		t.Fatalf("unexpected accounts payload: %s", rec.Body.String())
	}
	if payload.Accounts[0].DeviceIDPreview == "" || payload.Accounts[0].DeviceIDUpdatedAt == "" {
		t.Fatalf("expected masked preview and updated_at in %s", rec.Body.String())
	}
}
