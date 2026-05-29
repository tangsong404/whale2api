package pooldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"whale2api/internal/config"
)

var (
	ErrInvalidAPIKey  = errors.New("invalid API key: not registered in gateway pool")
	ErrAPIKeyDisabled = errors.New("api key is disabled")
)

// GatewayKeyExists reports whether the key is registered (ignores enabled flag).
func (db *DB) GatewayKeyExists(ctx context.Context, apiKey string) (bool, error) {
	if err := db.configured(); err != nil {
		return false, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return false, nil
	}
	var one int
	err := db.sql.QueryRowContext(ctx, `
SELECT 1 FROM gateway_api_keys WHERE api_key = ?
`, apiKey).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// LoadAccountsForAPIKey returns the DeepSeek account pool bound to apiKey (enabled keys only).
func (db *DB) LoadAccountsForAPIKey(ctx context.Context, apiKey string) ([]config.Account, error) {
	if err := db.configured(); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, ErrInvalidAPIKey
	}
	var enabled int
	err := db.sql.QueryRowContext(ctx, `
SELECT enabled FROM gateway_api_keys WHERE api_key = ?
`, apiKey).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidAPIKey
	}
	if err != nil {
		return nil, err
	}
	if enabled == 0 {
		return nil, ErrAPIKeyDisabled
	}
	rows, err := db.sql.QueryContext(ctx, `
SELECT pa.identifier, pa.password, pa.token
FROM pool_bindings pb
INNER JOIN pool_accounts pa ON pa.id = pb.account_id
WHERE pb.api_key = ? AND COALESCE(pa.discarded, 0) = 0
ORDER BY pb.position ASC, pa.id ASC
`, apiKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []config.Account
	for rows.Next() {
		var identifier, password, token string
		if err := rows.Scan(&identifier, &password, &token); err != nil {
			return nil, err
		}
		id := strings.TrimSpace(identifier)
		if id == "" {
			continue
		}
		out = append(out, config.Account{
			Email:    id,
			Password: password,
			Token:    token,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return limitAccounts(out), nil
}

// UpdateAccountToken persists a refreshed upstream token.
func (db *DB) UpdateAccountToken(ctx context.Context, identifier, token string) error {
	if err := db.configured(); err != nil {
		return err
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return fmt.Errorf("empty identifier")
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE pool_accounts SET token = ? WHERE identifier = ?`, token, identifier)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("pool account not found for identifier %q", identifier)
	}
	return nil
}

// ClearAccountToken clears cached upstream token.
func (db *DB) ClearAccountToken(ctx context.Context, identifier string) error {
	if err := db.configured(); err != nil {
		return err
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil
	}
	_, err := db.sql.ExecContext(ctx, `UPDATE pool_accounts SET token = ? WHERE identifier = ?`, "", identifier)
	return err
}
