package config

import "testing"

func TestThinkingInjectionEnabledDefaultsOff(t *testing.T) {
	store := &Store{}
	if store.ThinkingInjectionEnabled() {
		t.Fatal("expected thinking injection default off when unset")
	}
}
