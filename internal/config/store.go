package config

import (
	"errors"
	"sync"
)

type Store struct {
	mu  sync.RWMutex
	cfg Config
}

func LoadStore() *Store {
	store, err := loadStore()
	if err != nil {
		Logger.Warn("[config] load failed", "error", err)
	}
	return store
}

func LoadStoreWithError() (*Store, error) {
	return loadStore()
}

func loadStore() (*Store, error) {
	cfg := defaultConfig()
	if validateErr := ValidateConfig(cfg); validateErr != nil {
		return nil, validateErr
	}
	return &Store{cfg: cfg}, nil
}

func defaultConfig() Config {
	return Config{
		AutoDelete: AutoDeleteConfig{Mode: "none"},
	}
}

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Clone()
}

func (s *Store) Replace(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ValidateConfig(cfg); err != nil {
		return err
	}
	s.cfg = cfg.Clone()
	return nil
}

func (s *Store) Update(mutator func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.cfg.Clone()
	if err := mutator(&cfg); err != nil {
		return err
	}
	if err := ValidateConfig(cfg); err != nil {
		return err
	}
	s.cfg = cfg
	return nil
}

// SetProxies replaces proxy list (runtime-only; not persisted to JSON config).
func (s *Store) SetProxies(proxies []Proxy) error {
	if s == nil {
		return errors.New("config store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized := make([]Proxy, 0, len(proxies))
	for _, p := range proxies {
		normalized = append(normalized, NormalizeProxy(p))
	}
	if err := ValidateProxyConfig(normalized); err != nil {
		return err
	}
	s.cfg.Proxies = normalized
	return nil
}
