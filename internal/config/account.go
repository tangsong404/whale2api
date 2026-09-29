package config

import "strings"

func (a Account) Identifier() string {
	// PoolIdentifier preserves the raw pool_accounts identifier so lookups are
	// not affected by email/mobile normalization.
	if id := strings.TrimSpace(a.PoolIdentifier); id != "" {
		return id
	}
	if strings.TrimSpace(a.Email) != "" {
		return strings.TrimSpace(a.Email)
	}
	if mobile := NormalizeMobileForStorage(a.Mobile); mobile != "" {
		return mobile
	}
	return ""
}
