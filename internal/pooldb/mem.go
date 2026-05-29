package pooldb

import (
	"context"
	"strings"
	"sync"

	"whale2api/internal/config"
)

// Mem is an in-memory GatewayPool for unit tests.
type Mem struct {
	mu       sync.RWMutex
	keys     map[string]bool
	pools    map[string][]config.Account
	tokens   map[string]string
	discard  map[string]map[string]string // api_key -> identifier -> reason
}

func NewMem() *Mem {
	return &Mem{
		keys:    map[string]bool{},
		pools:   map[string][]config.Account{},
		tokens:  map[string]string{},
		discard: map[string]map[string]string{},
	}
}

func (m *Mem) RegisterKey(apiKey string, accounts []config.Account, enabled bool) {
	if m == nil {
		return
	}
	apiKey = strings.TrimSpace(apiKey)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[apiKey] = enabled
	cp := make([]config.Account, len(accounts))
	copy(cp, accounts)
	m.pools[apiKey] = cp
	for _, acc := range cp {
		id := acc.Identifier()
		if id != "" && acc.Token != "" {
			m.tokens[id] = acc.Token
		}
	}
}

func (m *Mem) GatewayKeyExists(_ context.Context, apiKey string) (bool, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return false, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.keys[apiKey]
	return ok, nil
}

func (m *Mem) LoadAccountsForAPIKey(_ context.Context, apiKey string) ([]config.Account, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, ErrInvalidAPIKey
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	enabled, ok := m.keys[apiKey]
	if !ok {
		return nil, ErrInvalidAPIKey
	}
	if !enabled {
		return nil, ErrAPIKeyDisabled
	}
	src := m.pools[apiKey]
	out := make([]config.Account, len(src))
	for i, acc := range src {
		id := acc.Identifier()
		if tok, ok := m.tokens[id]; ok {
			acc.Token = tok
		}
		out[i] = acc
	}
	return limitAccounts(out), nil
}

func (m *Mem) UpdateAccountToken(_ context.Context, identifier, token string) error {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[identifier] = token
	for key, accounts := range m.pools {
		for i, acc := range accounts {
			if acc.Identifier() == identifier {
				acc.Token = token
				m.pools[key][i] = acc
			}
		}
	}
	return nil
}

func (m *Mem) ClearAccountToken(ctx context.Context, identifier string) error {
	return m.UpdateAccountToken(ctx, identifier, "")
}

func (m *Mem) SetAccountPoolState(_ context.Context, apiKey, identifier string, discarded bool, reason string) error {
	apiKey = strings.TrimSpace(apiKey)
	identifier = strings.TrimSpace(identifier)
	if apiKey == "" || identifier == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.discard[apiKey] == nil {
		m.discard[apiKey] = map[string]string{}
	}
	if discarded {
		m.discard[apiKey][identifier] = strings.TrimSpace(reason)
	} else {
		delete(m.discard[apiKey], identifier)
	}
	return nil
}
