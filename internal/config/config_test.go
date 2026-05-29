package config

import (
	"testing"
)

func TestDefaultConfigValidates(t *testing.T) {
	if err := ValidateConfig(defaultConfig()); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestStoreSetProxies(t *testing.T) {
	store := LoadStore()
	if err := store.SetProxies([]Proxy{{
		ID:   "proxy-sh-1",
		Type: "socks5h",
		Host: "127.0.0.1",
		Port: 1080,
	}}); err != nil {
		t.Fatalf("SetProxies: %v", err)
	}
	snap := store.Snapshot()
	if len(snap.Proxies) != 1 || snap.Proxies[0].ID != "proxy-sh-1" {
		t.Fatalf("unexpected proxies: %#v", snap.Proxies)
	}
}

func TestValidateConfigRejectsInvalidRuntime(t *testing.T) {
	err := ValidateConfig(Config{Runtime: RuntimeConfig{AccountMaxInflight: 8, GlobalMaxInflight: 4}})
	if err == nil {
		t.Fatal("expected validation error")
	}
}
