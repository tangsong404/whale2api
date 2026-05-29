package pooldb

import (
	"context"
	"strings"

	"whale2api/internal/config"
)

// SeedGatewayPool ensures apiKey exists with the given accounts (for integration tests).
func (db *DB) SeedGatewayPool(ctx context.Context, apiKey string, accounts []config.Account) error {
	if db == nil || db.sql == nil {
		return nil
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil
	}
	exists, err := db.GatewayKeyExists(ctx, apiKey)
	if err != nil {
		return err
	}
	if !exists {
		if err := db.CreateGatewayKey(ctx, apiKey, "", "test-seed"); err != nil {
			return err
		}
	}
	for _, acc := range accounts {
		id := acc.Identifier()
		if id == "" {
			continue
		}
		_ = db.AddAccountToPool(ctx, apiKey, id, acc.Password)
		if tok := strings.TrimSpace(acc.Token); tok != "" {
			_ = db.UpdateAccountToken(ctx, id, tok)
		}
	}
	return nil
}
