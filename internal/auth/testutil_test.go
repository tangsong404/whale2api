package auth

import (
	"context"
	"testing"

	"whale2api/internal/config"
	"whale2api/internal/pooldb"
)

func newTestResolver(t *testing.T) *Resolver {
	return newTestResolverWithAccounts(t, "managed-key", []config.Account{{
		Email:    "acc@example.com",
		Password: "pwd",
		Token:    "account-token",
	}}, func(_ context.Context, _ config.Account) (string, error) {
		return "fresh-token", nil
	})
}

func newTestResolverWithAccounts(t *testing.T, apiKey string, accounts []config.Account, login LoginFunc) *Resolver {
	t.Helper()
	store := config.LoadStore()
	mem := pooldb.NewMem()
	mem.RegisterKey(apiKey, accounts, true)
	r := NewResolver(store, login)
	r.PoolDB = mem
	return r
}
