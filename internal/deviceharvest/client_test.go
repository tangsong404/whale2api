package deviceharvest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHarvestSuccess(t *testing.T) {
	var gotPath, gotContentType, gotSharedToken string
	var gotBody harvestRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotSharedToken = r.Header.Get("X-Harvest-Token")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"device_id":"B-harvested-token"}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "shared-secret", time.Second)
	token, err := c.Harvest(context.Background(), "acc@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if token != "B-harvested-token" {
		t.Fatalf("token = %q", token)
	}
	if gotPath != "/harvest" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content-type = %q", gotContentType)
	}
	if gotSharedToken != "shared-secret" {
		t.Fatalf("X-Harvest-Token = %q", gotSharedToken)
	}
	if gotBody.AccountID != "acc@example.com" {
		t.Fatalf("account_id = %q", gotBody.AccountID)
	}
}

func TestHarvestFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "non-2xx", status: http.StatusInternalServerError, body: `{"error":"boom"}`},
		{name: "empty token", status: http.StatusOK, body: `{"device_id":""}`},
		{name: "bad json", status: http.StatusOK, body: `not-json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			c := New(srv.URL, "", time.Second)
			_, err := c.Harvest(context.Background(), "acc@example.com")
			if !errors.Is(err, ErrDeviceHarvestUnavailable) {
				t.Fatalf("expected ErrDeviceHarvestUnavailable, got %v", err)
			}
		})
	}
}

func TestHarvestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"device_id":"B-late"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "", 30*time.Millisecond)
	_, err := c.Harvest(context.Background(), "acc@example.com")
	if !errors.Is(err, ErrDeviceHarvestUnavailable) {
		t.Fatalf("expected ErrDeviceHarvestUnavailable on timeout, got %v", err)
	}
}

func TestHarvestDisabled(t *testing.T) {
	c := New("", "secret", time.Second)
	if c.Enabled() {
		t.Fatal("empty URL must be disabled")
	}
	if _, err := c.Harvest(context.Background(), "acc@example.com"); !errors.Is(err, ErrDeviceHarvestUnavailable) {
		t.Fatalf("expected ErrDeviceHarvestUnavailable, got %v", err)
	}
}

func TestDefaultTimeoutOutlastsSidecarDeadline(t *testing.T) {
	// tools/device-harvest/harvest.mjs waits TOKEN_DEADLINE_MS (30s) for a "B"
	// token before it responds; the client must outlast that plus queue time.
	if DefaultTimeout <= sidecarTokenDeadline {
		t.Fatalf("DefaultTimeout = %v, want > sidecar deadline %v", DefaultTimeout, sidecarTokenDeadline)
	}
	if sidecarTokenDeadline != 30*time.Second {
		t.Fatalf("sidecarTokenDeadline = %v, want 30s to match TOKEN_DEADLINE_MS", sidecarTokenDeadline)
	}
	// A zero timeout means "use the default"; New must not silently pick a
	// shorter deadline than the sidecar's own.
	c := New("http://device-harvest.invalid", "", 0)
	if got := c.http.Timeout; got != DefaultTimeout {
		t.Fatalf("New(_, _, 0).http.Timeout = %v, want %v", got, DefaultTimeout)
	}
}

func TestNewFromEnv(t *testing.T) {
	t.Setenv("DEVICE_HARVEST_URL", "http://device-harvest:8090/")
	t.Setenv("DEVICE_HARVEST_TOKEN", "tok")
	c := NewFromEnv()
	if !c.Enabled() {
		t.Fatal("expected enabled client")
	}
	if c.baseURL != "http://device-harvest:8090" {
		t.Fatalf("baseURL = %q", c.baseURL)
	}
	if c.token != "tok" {
		t.Fatalf("token = %q", c.token)
	}

	t.Setenv("DEVICE_HARVEST_URL", "")
	if NewFromEnv().Enabled() {
		t.Fatal("empty env URL must yield a disabled client")
	}
}
