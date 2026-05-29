package pooldb

import (
	"strings"
	"testing"
)

func TestGenerateGatewayAPIKeyPrefix(t *testing.T) {
	k, err := GenerateGatewayAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k, "sk-") {
		t.Fatalf("expected sk- prefix, got %q", k)
	}
	if len(k) < 20 {
		t.Fatalf("key too short: %q", k)
	}
}

func TestNormalizeOrGenerateGatewayAPIKey(t *testing.T) {
	got, err := NormalizeOrGenerateGatewayAPIKey("  custom-key  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "custom-key" {
		t.Fatalf("got %q", got)
	}
	gen, err := NormalizeOrGenerateGatewayAPIKey("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gen, "sk-") {
		t.Fatalf("got %q", gen)
	}
}
