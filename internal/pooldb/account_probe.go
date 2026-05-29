package pooldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"whale2api/internal/config"
)

// PoolAccountCredential is login material for a bound pool account.
type PoolAccountCredential struct {
	Identifier string
	Password   string
	Discarded  bool
}

// GetPoolAccountCredential loads one account bound to apiKey.
func (db *DB) GetPoolAccountCredential(ctx context.Context, apiKey, identifier string) (PoolAccountCredential, error) {
	if err := db.configured(); err != nil {
		return PoolAccountCredential{}, err
	}
	apiKey = strings.TrimSpace(apiKey)
	identifier = strings.TrimSpace(identifier)
	var cred PoolAccountCredential
	var discarded int
	err := db.sql.QueryRowContext(ctx, `
SELECT pa.identifier, pa.password, COALESCE(pa.discarded, 0)
FROM pool_bindings pb
INNER JOIN pool_accounts pa ON pa.id = pb.account_id
WHERE pb.api_key = ? AND pa.identifier = ?
`, apiKey, identifier).Scan(&cred.Identifier, &cred.Password, &discarded)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PoolAccountCredential{}, fmt.Errorf("account not in pool")
		}
		return PoolAccountCredential{}, err
	}
	cred.Discarded = discarded != 0
	return cred, nil
}

// ListPoolAccountCredentials returns credentials for bound accounts (optionally filtered).
func (db *DB) ListPoolAccountCredentials(ctx context.Context, apiKey string, identifiers []string, activeOnly bool) ([]PoolAccountCredential, error) {
	if err := db.configured(); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	q := `
SELECT pa.identifier, pa.password, COALESCE(pa.discarded, 0)
FROM pool_bindings pb
INNER JOIN pool_accounts pa ON pa.id = pb.account_id
WHERE pb.api_key = ?`
	if activeOnly {
		q += ` AND COALESCE(pa.discarded, 0) = 0`
	}
	q += ` ORDER BY pb.position ASC, pa.id ASC`
	rows, err := db.sql.QueryContext(ctx, q, apiKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	want := map[string]struct{}{}
	for _, id := range identifiers {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = struct{}{}
		}
	}
	filter := len(want) > 0
	var out []PoolAccountCredential
	for rows.Next() {
		var cred PoolAccountCredential
		var discarded int
		if err := rows.Scan(&cred.Identifier, &cred.Password, &discarded); err != nil {
			return nil, err
		}
		cred.Discarded = discarded != 0
		if filter {
			if _, ok := want[cred.Identifier]; !ok {
				continue
			}
		}
		out = append(out, cred)
	}
	return out, rows.Err()
}

// AccountToConfig maps stored identifier + password to config.Account for DeepSeek login.
func AccountToConfig(identifier, password string) config.Account {
	identifier = strings.TrimSpace(identifier)
	acc := config.Account{Password: strings.TrimSpace(password)}
	if strings.Contains(identifier, "@") {
		acc.Email = identifier
	} else {
		acc.Mobile = identifier
	}
	return acc
}
