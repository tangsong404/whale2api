package pooldb

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
)

// GenerateGatewayAPIKey returns a random client key with an sk- prefix.
func GenerateGatewayAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sk-" + base64.RawURLEncoding.EncodeToString(b), nil
}

// NormalizeOrGenerateGatewayAPIKey trims input or generates sk-… when empty.
func NormalizeOrGenerateGatewayAPIKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key != "" {
		return key, nil
	}
	return GenerateGatewayAPIKey()
}
