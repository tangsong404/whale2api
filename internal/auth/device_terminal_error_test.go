package auth_test

import (
	"context"
	"errors"
	"testing"

	"whale2api/internal/auth"
	"whale2api/internal/config"
	"whale2api/internal/deepseek/client"
	"whale2api/internal/pooldb"
)

// staticHarvester always returns the same token; enough to exercise the
// clear -> re-harvest -> retry path.
type staticHarvester struct{ token string }

func (h *staticHarvester) Enabled() bool { return true }

func (h *staticHarvester) Harvest(context.Context, string) (string, error) { return h.token, nil }

// TestTerminalDeviceErrorKeepsStructuredRejection verifies that the terminal
// ErrNoUsableDeviceToken error still wraps the underlying client.RequestFailure
// so errors.Is/errors.As and client.IsDeviceRejectedError work for callers.
func TestTerminalDeviceErrorKeepsStructuredRejection(t *testing.T) {
	r := auth.NewResolver(config.LoadStore(), func(context.Context, config.Account) (string, error) {
		return "", &client.RequestFailure{
			Op:      "login",
			Kind:    client.FailureDeviceRejected,
			Message: "RISK_DEVICE_DETECTED",
		}
	})
	mem := pooldb.NewMem()
	mem.RegisterKey("managed-key", []config.Account{{Email: "acc@example.com", Password: "pwd"}}, true)
	r.PoolDB = mem
	r.DeviceHarvest = &staticHarvester{token: "B-fresh"}

	_, err := r.LoginWithDevice(context.Background(), config.Account{Email: "acc@example.com", Password: "pwd"})
	if !errors.Is(err, auth.ErrNoUsableDeviceToken) {
		t.Fatalf("expected ErrNoUsableDeviceToken, got %v", err)
	}
	if !client.IsDeviceRejectedError(err) {
		t.Fatalf("terminal error lost the structured device rejection: %v", err)
	}
	var failure *client.RequestFailure
	if !errors.As(err, &failure) || failure.Kind != client.FailureDeviceRejected {
		t.Fatalf("errors.As did not recover RequestFailure: %v", err)
	}
}
