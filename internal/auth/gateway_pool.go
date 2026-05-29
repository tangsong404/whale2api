package auth

import (
	"context"

	"whale2api/internal/config"
)

// GatewayPool is the SQLite-backed (or test in-memory) gateway key → account store.
type GatewayPool interface {
	LoadAccountsForAPIKey(ctx context.Context, apiKey string) ([]config.Account, error)
	GatewayKeyExists(ctx context.Context, apiKey string) (bool, error)
	UpdateAccountToken(ctx context.Context, identifier, token string) error
	ClearAccountToken(ctx context.Context, identifier string) error
}

// PoolAdmin updates per-key account pool state (discard / restore).
type PoolAdmin interface {
	SetAccountPoolState(ctx context.Context, apiKey, identifier string, discarded bool, reason string) error
}
