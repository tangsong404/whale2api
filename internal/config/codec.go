package config

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

func (c Config) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	for k, v := range c.AdditionalFields {
		m[k] = v
	}
	if len(c.Proxies) > 0 {
		m["proxies"] = c.Proxies
	}
	if c.Runtime.AccountMaxInflight > 0 || c.Runtime.AccountMaxQueue > 0 || c.Runtime.GlobalMaxInflight > 0 || c.Runtime.TokenRefreshIntervalHours > 0 {
		m["runtime"] = c.Runtime
	}
	if c.Responses.StoreTTLSeconds > 0 {
		m["responses"] = c.Responses
	}
	if strings.TrimSpace(c.Embeddings.Provider) != "" {
		m["embeddings"] = c.Embeddings
	}
	m["auto_delete"] = c.AutoDelete
	if c.CurrentInputFile.Enabled != nil || c.CurrentInputFile.MinChars > 0 {
		m["current_input_file"] = c.CurrentInputFile
	}
	if c.ThinkingInjection.Enabled != nil || strings.TrimSpace(c.ThinkingInjection.Prompt) != "" {
		m["thinking_injection"] = c.ThinkingInjection
	}
	return json.Marshal(m)
}

func (c *Config) UnmarshalJSON(b []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.AdditionalFields = map[string]any{}
	for k, v := range raw {
		switch k {
		case "keys", "api_keys", "accounts", "admin":
			// Removed gateway pool / admin fields; ignored instead of persisted.
		case "proxies":
			if err := json.Unmarshal(v, &c.Proxies); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "claude_mapping", "claude_model_mapping", "model_aliases", "compat", "toolcall", "history_split", "_vercel_sync_hash", "_vercel_sync_time":
			// Legacy fields ignored.
		case "runtime":
			if err := json.Unmarshal(v, &c.Runtime); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "responses":
			if err := json.Unmarshal(v, &c.Responses); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "embeddings":
			if err := json.Unmarshal(v, &c.Embeddings); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "auto_delete":
			if err := json.Unmarshal(v, &c.AutoDelete); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "current_input_file":
			if err := json.Unmarshal(v, &c.CurrentInputFile); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "thinking_injection":
			if err := json.Unmarshal(v, &c.ThinkingInjection); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		default:
			var anyVal any
			if err := json.Unmarshal(v, &anyVal); err == nil {
				c.AdditionalFields[k] = anyVal
			}
		}
	}
	return nil
}

func (c Config) Clone() Config {
	clone := Config{
		Proxies:   slices.Clone(c.Proxies),
		Runtime:   c.Runtime,
		Responses: c.Responses,
		Embeddings: EmbeddingsConfig{
			Provider: c.Embeddings.Provider,
		},
		AutoDelete: c.AutoDelete,
		CurrentInputFile: CurrentInputFileConfig{
			Enabled:  cloneBoolPtr(c.CurrentInputFile.Enabled),
			MinChars: c.CurrentInputFile.MinChars,
		},
		ThinkingInjection: ThinkingInjectionConfig{
			Enabled: cloneBoolPtr(c.ThinkingInjection.Enabled),
			Prompt:  c.ThinkingInjection.Prompt,
		},
		AdditionalFields: map[string]any{},
	}
	for k, v := range c.AdditionalFields {
		clone.AdditionalFields[k] = v
	}
	return clone
}

func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}
