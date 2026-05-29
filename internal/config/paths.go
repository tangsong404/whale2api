package config

import (
	"os"
	"path/filepath"
	"strings"
)

func BaseDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func ResolvePath(envKey, defaultRel string) string {
	raw := strings.TrimSpace(os.Getenv(envKey))
	if raw != "" {
		if filepath.IsAbs(raw) {
			return raw
		}
		return filepath.Join(BaseDir(), raw)
	}
	return filepath.Join(BaseDir(), defaultRel)
}

func ChatHistoryPath() string {
	return ResolvePath("WHALE2API_CHAT_HISTORY_PATH", "data/chat_history.json")
}
