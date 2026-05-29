package client

import (
	"context"
	"testing"
)

func TestPreloadPowNoOp(t *testing.T) {
	client := NewClient(nil, nil)
	if err := client.PreloadPow(context.Background()); err != nil {
		t.Fatalf("PreloadPow should be no-op, got error: %v", err)
	}
}

func TestComputePowUnsupportedAlgorithm(t *testing.T) {
	_, err := ComputePow(context.Background(), map[string]any{"algorithm": "unknown"})
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}
