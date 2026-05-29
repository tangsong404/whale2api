package account

import (
	"whale2api/internal/config"
)

func newTestPool(accounts []config.Account, runtime *config.Store) *Pool {
	return NewPoolWithRuntime(NewMemoryLookup(accounts), runtime)
}
