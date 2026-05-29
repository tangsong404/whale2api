package config

import (
	"os"
	"strconv"
	"strings"
)

func (s *Store) ToolcallMode() string {
	return "feature_match"
}

func (s *Store) ToolcallEarlyEmitConfidence() string {
	return "high"
}

func (s *Store) ResponsesStoreTTLSeconds() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Responses.StoreTTLSeconds > 0 {
		return s.cfg.Responses.StoreTTLSeconds
	}
	return 900
}

func (s *Store) EmbeddingsProvider() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.Embeddings.Provider)
}

func (s *Store) AutoDeleteMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mode := strings.ToLower(strings.TrimSpace(s.cfg.AutoDelete.Mode))
	switch mode {
	case "none", "single", "all":
		return mode
	}
	if s.cfg.AutoDelete.Sessions {
		return "all"
	}
	return "none"
}

func (s *Store) RuntimeAccountMaxInflight() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Runtime.AccountMaxInflight > 0 {
		return s.cfg.Runtime.AccountMaxInflight
	}
	if raw := strings.TrimSpace(os.Getenv("WHALE2API_ACCOUNT_MAX_INFLIGHT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

func (s *Store) RuntimeAccountMaxQueue(defaultSize int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Runtime.AccountMaxQueue > 0 {
		return s.cfg.Runtime.AccountMaxQueue
	}
	if raw := strings.TrimSpace(os.Getenv("WHALE2API_ACCOUNT_MAX_QUEUE")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	if defaultSize < 0 {
		return 0
	}
	return defaultSize
}

func (s *Store) RuntimeGlobalMaxInflight(defaultSize int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Runtime.GlobalMaxInflight > 0 {
		return s.cfg.Runtime.GlobalMaxInflight
	}
	if raw := strings.TrimSpace(os.Getenv("WHALE2API_GLOBAL_MAX_INFLIGHT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if defaultSize < 0 {
		return 0
	}
	return defaultSize
}

func (s *Store) RuntimeTokenRefreshIntervalHours() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Runtime.TokenRefreshIntervalHours > 0 {
		return s.cfg.Runtime.TokenRefreshIntervalHours
	}
	return 6
}

func (s *Store) AutoDeleteSessions() bool {
	return s.AutoDeleteMode() != "none"
}

func (s *Store) CurrentInputFileEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.CurrentInputFile.Enabled == nil {
		return true
	}
	return *s.cfg.CurrentInputFile.Enabled
}

func (s *Store) CurrentInputFileMinChars() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.CurrentInputFile.MinChars > 0 {
		return s.cfg.CurrentInputFile.MinChars
	}
	return 6000
}

func (s *Store) ThinkingInjectionEnabled() bool {
	if v, ok := thinkingInjectionEnabledFromEnv(); ok {
		return v
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.ThinkingInjection.Enabled == nil {
		return true
	}
	return *s.cfg.ThinkingInjection.Enabled
}

func (s *Store) ThinkingInjectionPrompt() string {
	if v, ok := thinkingInjectionPromptFromEnv(); ok {
		return v
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.ThinkingInjection.Prompt)
}

// Env vars (repo-root .env via compose env_file, or process env) override thinking_injection in runtime config.
const (
	envThinkingInjectionEnabled = "WHALE2API_THINKING_INJECTION_ENABLED"
	envThinkingInjectionPrompt  = "WHALE2API_THINKING_INJECTION_PROMPT"
)

func thinkingInjectionEnabledFromEnv() (value bool, override bool) {
	raw, ok := os.LookupEnv(envThinkingInjectionEnabled)
	if !ok {
		return false, false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, false
	}
	return parseBoolEnv(raw), true
}

func thinkingInjectionPromptFromEnv() (value string, override bool) {
	raw, ok := os.LookupEnv(envThinkingInjectionPrompt)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(raw), true
}

func parseBoolEnv(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
