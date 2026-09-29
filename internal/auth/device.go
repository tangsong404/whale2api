package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"whale2api/internal/config"
	"whale2api/internal/deviceharvest"
)

var (
	// ErrDeviceHarvestUnavailable is returned when a device token is required but
	// the harvest service cannot provide one (not configured, timeout, bad response).
	ErrDeviceHarvestUnavailable = deviceharvest.ErrDeviceHarvestUnavailable
	// ErrNoUsableDeviceToken is returned when DeepSeek rejected two freshly
	// harvested device tokens in a row. It must not be classified as mute/ban.
	ErrNoUsableDeviceToken = errors.New("no usable device token: DeepSeek device risk control rejected the fingerprint twice")
)

// DeviceHarvester produces a fresh Shumei device token for an account.
type DeviceHarvester interface {
	Harvest(ctx context.Context, accountID string) (string, error)
	Enabled() bool
}

type deviceHarvestCall struct {
	done  chan struct{}
	token string
	err   error
}

// deviceRejection is the consumer-side interface satisfied by
// client.RequestFailure when DeepSeek rejects the device fingerprint.
type deviceRejection interface {
	DeviceRejected() bool
}

// EnsureAccountDeviceID returns acc with a device token: the stored one when
// present, otherwise a freshly harvested and persisted one. When no harvest
// service is configured it keeps the legacy behavior (empty token, one warning)
// so startup and logins are never blocked by the missing service.
func (r *Resolver) EnsureAccountDeviceID(ctx context.Context, acc config.Account) (config.Account, error) {
	if r == nil {
		return acc, nil
	}
	if strings.TrimSpace(acc.DeviceID) != "" {
		return acc, nil
	}
	harvester := r.DeviceHarvest
	if harvester == nil || !harvester.Enabled() {
		r.harvestWarnOnce.Do(func() {
			config.Logger.Warn("[device] harvest service not configured; logging in without a device token (legacy behavior)")
		})
		return acc, nil
	}
	accountID := acc.Identifier()
	if accountID == "" {
		return acc, fmt.Errorf("%w: empty account identifier", ErrDeviceHarvestUnavailable)
	}
	token, err := r.harvestDeviceID(ctx, accountID)
	if err != nil {
		return acc, err
	}
	acc.DeviceID = token
	return acc, nil
}

// harvestDeviceID merges concurrent harvests for the same account and persists
// the result. It performs a single harvest attempt per call, never a retry loop.
func (r *Resolver) harvestDeviceID(ctx context.Context, accountID string) (string, error) {
	r.harvestMu.Lock()
	if r.harvestCalls == nil {
		r.harvestCalls = map[string]*deviceHarvestCall{}
	}
	if call := r.harvestCalls[accountID]; call != nil {
		r.harvestMu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-call.done:
			return call.token, call.err
		}
	}
	call := &deviceHarvestCall{done: make(chan struct{})}
	r.harvestCalls[accountID] = call
	r.harvestMu.Unlock()

	token, err := r.DeviceHarvest.Harvest(ctx, accountID)
	if err == nil {
		token = strings.TrimSpace(token)
		if token == "" {
			err = fmt.Errorf("%w: harvest returned an empty token", ErrDeviceHarvestUnavailable)
		} else if r.PoolDB != nil {
			if dbErr := r.PoolDB.UpdateAccountDeviceID(ctx, accountID, token); dbErr != nil {
				// The token is still usable for this login; the next request re-harvests.
				config.Logger.Warn("[device] persist harvested token failed", "account", accountID, "error", dbErr)
			} else {
				config.Logger.Info("[device] harvested device token", "account", accountID, "device_id", truncateDeviceToken(token))
			}
		}
	}

	r.harvestMu.Lock()
	call.token = token
	call.err = err
	delete(r.harvestCalls, accountID)
	close(call.done)
	r.harvestMu.Unlock()
	return token, err
}

// loginWithDevice harvests a device token on demand, logs in, and on device
// rejection clears the token, re-harvests once and retries. It returns the
// upstream token plus the (possibly refreshed) device id.
func (r *Resolver) loginWithDevice(ctx context.Context, acc config.Account) (string, string, error) {
	acc, err := r.EnsureAccountDeviceID(ctx, acc)
	if err != nil {
		return "", acc.DeviceID, err
	}
	token, err := r.Login(ctx, acc)
	if err == nil || !isDeviceRejectedError(err) {
		return token, acc.DeviceID, err
	}

	accountID := acc.Identifier()
	config.Logger.Warn("[login] device token rejected; re-harvesting once",
		"account", accountID, "device_id", truncateDeviceToken(acc.DeviceID))
	if r.PoolDB != nil && accountID != "" {
		if clearErr := r.PoolDB.ClearAccountDeviceID(ctx, accountID); clearErr != nil {
			config.Logger.Warn("[login] clear rejected device token failed", "account", accountID, "error", clearErr)
		}
	}

	acc.DeviceID = ""
	acc, err = r.EnsureAccountDeviceID(ctx, acc)
	if err != nil {
		return "", acc.DeviceID, err
	}
	token, err = r.Login(ctx, acc)
	if err != nil && isDeviceRejectedError(err) {
		return "", acc.DeviceID, fmt.Errorf("%w: %w", ErrNoUsableDeviceToken, err)
	}
	return token, acc.DeviceID, err
}

// LoginWithDevice logs in with the account device token, harvesting on demand
// and retrying once with a fresh token when DeepSeek rejects the device.
func (r *Resolver) LoginWithDevice(ctx context.Context, acc config.Account) (string, error) {
	token, _, err := r.loginWithDevice(ctx, acc)
	return token, err
}

func isDeviceRejectedError(err error) bool {
	if err == nil {
		return false
	}
	var rejected deviceRejection
	return errors.As(err, &rejected) && rejected.DeviceRejected()
}

// truncateDeviceToken keeps device tokens (fingerprint data) out of logs.
func truncateDeviceToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 12 {
		return token[:2] + "…"
	}
	return token[:4] + "…" + token[len(token)-4:]
}
