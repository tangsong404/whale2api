package pooldb

import "testing"

func TestAccountToConfigEmail(t *testing.T) {
	acc := AccountToConfig("u@example.com", "pass", "B-device-token")
	if acc.Email != "u@example.com" || acc.Mobile != "" {
		t.Fatalf("got %+v", acc)
	}
	if acc.DeviceID != "B-device-token" {
		t.Fatalf("expected device id to be copied, got %+v", acc)
	}
	if acc.PoolIdentifier != "u@example.com" || acc.Identifier() != "u@example.com" {
		t.Fatalf("expected raw pool identifier, got %+v (Identifier=%q)", acc, acc.Identifier())
	}
}

func TestAccountToConfigMobile(t *testing.T) {
	acc := AccountToConfig("13800138000", "pass", "")
	if acc.Mobile != "13800138000" || acc.Email != "" {
		t.Fatalf("got %+v", acc)
	}
	if acc.DeviceID != "" {
		t.Fatalf("expected empty device id, got %+v", acc)
	}
	// The login credential stays a raw mobile, but DB lookups must not use the
	// +86-normalized form.
	if acc.PoolIdentifier != "13800138000" {
		t.Fatalf("PoolIdentifier = %q, want raw", acc.PoolIdentifier)
	}
	if got := acc.Identifier(); got != "13800138000" {
		t.Fatalf("Identifier() = %q, want raw mobile", got)
	}
}
