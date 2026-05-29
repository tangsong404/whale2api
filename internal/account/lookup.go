package account

import (
	"strings"

	"whale2api/internal/config"
)

// Lookup is the account catalog used by Pool for acquire/release.
type Lookup interface {
	Accounts() []config.Account
	FindAccount(identifier string) (config.Account, bool)
}

// MemoryLookup is a fixed in-memory account set (e.g. SQL snapshot for one request).
type MemoryLookup struct {
	accounts []config.Account
	byID     map[string]config.Account
}

func NewMemoryLookup(accounts []config.Account) *MemoryLookup {
	filtered := make([]config.Account, 0, len(accounts))
	by := make(map[string]config.Account, len(accounts))
	for _, a := range accounts {
		id := a.Identifier()
		if id == "" {
			continue
		}
		filtered = append(filtered, a)
		by[id] = a
	}
	return &MemoryLookup{accounts: filtered, byID: by}
}

func (m *MemoryLookup) Accounts() []config.Account {
	return m.accounts
}

func (m *MemoryLookup) FindAccount(identifier string) (config.Account, bool) {
	a, ok := m.byID[strings.TrimSpace(identifier)]
	return a, ok
}
