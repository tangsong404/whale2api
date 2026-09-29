// Package deviceharvest talks to the optional device-harvest sidecar that
// produces real Shumei device tokens ("B...") with a headless browser.
package deviceharvest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ErrDeviceHarvestUnavailable marks any failure to obtain a device token from
// the harvest service (not configured, network error, timeout, bad response).
var ErrDeviceHarvestUnavailable = errors.New("device harvest service unavailable")

// sidecarTokenDeadline mirrors the sidecar's own token wait budget
// (tools/device-harvest/harvest.mjs TOKEN_DEADLINE_MS = 30s). The client must
// outwait the sidecar plus queue time, otherwise a slow-but-successful harvest
// would be aborted just before the sidecar responds.
const sidecarTokenDeadline = 30 * time.Second

// DefaultTimeout bounds a single harvest request. Real harvests take ~2s, but
// the sidecar may legitimately spend up to sidecarTokenDeadline waiting for a
// "B" token, so the client deadline stays above it with margin.
const DefaultTimeout = sidecarTokenDeadline + 5*time.Second

// Client is an HTTP client for the device-harvest sidecar.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New creates a client for baseURL. An empty baseURL yields a disabled client
// whose Harvest always fails with ErrDeviceHarvestUnavailable.
func New(baseURL, token string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		http:    &http.Client{Timeout: timeout},
	}
}

// NewFromEnv builds a client from DEVICE_HARVEST_URL and optional DEVICE_HARVEST_TOKEN.
func NewFromEnv() *Client {
	return New(os.Getenv("DEVICE_HARVEST_URL"), os.Getenv("DEVICE_HARVEST_TOKEN"), DefaultTimeout)
}

// Enabled reports whether a harvest service URL is configured.
func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

type harvestRequest struct {
	AccountID string `json:"account_id"`
}

type harvestResponse struct {
	DeviceID string `json:"device_id"`
}

// Harvest requests a fresh device token for accountID. It performs exactly one
// attempt; callers decide whether retrying is appropriate.
func (c *Client) Harvest(ctx context.Context, accountID string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("%w: DEVICE_HARVEST_URL is not configured", ErrDeviceHarvestUnavailable)
	}
	body, err := json.Marshal(harvestRequest{AccountID: strings.TrimSpace(accountID)})
	if err != nil {
		return "", fmt.Errorf("%w: encode request: %v", ErrDeviceHarvestUnavailable, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/harvest", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: build request: %v", ErrDeviceHarvestUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Harvest-Token", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDeviceHarvestUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("%w: read response: %v", ErrDeviceHarvestUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: HTTP %d: %s", ErrDeviceHarvestUnavailable, resp.StatusCode, truncate(string(raw), 200))
	}
	var out harvestResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("%w: decode response: %v", ErrDeviceHarvestUnavailable, err)
	}
	deviceID := strings.TrimSpace(out.DeviceID)
	if deviceID == "" {
		return "", fmt.Errorf("%w: empty device_id in response", ErrDeviceHarvestUnavailable)
	}
	return deviceID, nil
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
