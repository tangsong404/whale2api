package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountDeviceIDJSONRoundTrip(t *testing.T) {
	raw, err := json.Marshal(Account{Email: "u@example.com", Password: "pwd", DeviceID: "B-device-token"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"device_id":"B-device-token"`) {
		t.Fatalf("device_id missing from JSON: %s", raw)
	}
	var got Account
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != "B-device-token" {
		t.Fatalf("device_id = %q", got.DeviceID)
	}
}

func TestAccountDeviceIDOmittedWhenEmpty(t *testing.T) {
	raw, err := json.Marshal(Account{Email: "u@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "device_id") {
		t.Fatalf("empty device_id should be omitted: %s", raw)
	}
}

func TestAccountIdentifierPrefersPoolIdentifier(t *testing.T) {
	raw := Account{Mobile: "13800138000", PoolIdentifier: "13800138000"}
	if got := raw.Identifier(); got != "13800138000" {
		t.Fatalf("Identifier() = %q, want the raw pool identifier", got)
	}
	normalized := Account{Mobile: "13800138000"}
	if got := normalized.Identifier(); got != "+8613800138000" {
		t.Fatalf("Identifier() without PoolIdentifier = %q, want normalized mobile", got)
	}
	email := Account{Email: "u@example.com", PoolIdentifier: "raw-id"}
	if got := email.Identifier(); got != "raw-id" {
		t.Fatalf("Identifier() = %q, want %q", got, "raw-id")
	}
}

func TestAccountPoolIdentifierNotSerialized(t *testing.T) {
	raw, err := json.Marshal(Account{Email: "u@example.com", PoolIdentifier: "raw-pool-id"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "raw-pool-id") || strings.Contains(string(raw), "PoolIdentifier") {
		t.Fatalf("PoolIdentifier must not be serialized: %s", raw)
	}
}
