package config

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

type Config struct {
	Proxies           []Proxy                 `json:"proxies,omitempty"`
	Runtime           RuntimeConfig           `json:"runtime,omitempty"`
	Responses         ResponsesConfig         `json:"responses,omitempty"`
	Embeddings        EmbeddingsConfig        `json:"embeddings,omitempty"`
	AutoDelete        AutoDeleteConfig        `json:"auto_delete"`
	CurrentInputFile  CurrentInputFileConfig  `json:"current_input_file,omitempty"`
	ThinkingInjection ThinkingInjectionConfig `json:"thinking_injection,omitempty"`
	AdditionalFields  map[string]any          `json:"-"`
}

type Account struct {
	Name     string `json:"name,omitempty"`
	Remark   string `json:"remark,omitempty"`
	Email    string `json:"email,omitempty"`
	Mobile   string `json:"mobile,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	ProxyID  string `json:"proxy_id,omitempty"`
}

type Proxy struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func NormalizeProxy(p Proxy) Proxy {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	p.Host = strings.TrimSpace(p.Host)
	p.Username = strings.TrimSpace(p.Username)
	p.Password = strings.TrimSpace(p.Password)
	if p.ID == "" {
		p.ID = StableProxyID(p)
	}
	if p.Name == "" && p.Host != "" && p.Port > 0 {
		p.Name = fmt.Sprintf("%s:%d", p.Host, p.Port)
	}
	return p
}

func StableProxyID(p Proxy) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(p.Type)) + "|" + strings.ToLower(strings.TrimSpace(p.Host)) + "|" + fmt.Sprintf("%d", p.Port) + "|" + strings.TrimSpace(p.Username)))
	return "proxy_" + hex.EncodeToString(sum[:6])
}

type RuntimeConfig struct {
	AccountMaxInflight        int `json:"account_max_inflight,omitempty"`
	AccountMaxQueue           int `json:"account_max_queue,omitempty"`
	GlobalMaxInflight         int `json:"global_max_inflight,omitempty"`
	TokenRefreshIntervalHours int `json:"token_refresh_interval_hours,omitempty"`
}

type ResponsesConfig struct {
	StoreTTLSeconds int `json:"store_ttl_seconds,omitempty"`
}

type EmbeddingsConfig struct {
	Provider string `json:"provider,omitempty"`
}

type AutoDeleteConfig struct {
	Mode     string `json:"mode,omitempty"`
	Sessions bool   `json:"sessions,omitempty"`
}

type CurrentInputFileConfig struct {
	Enabled  *bool `json:"enabled,omitempty"`
	MinChars int   `json:"min_chars,omitempty"`
}

type ThinkingInjectionConfig struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
}
