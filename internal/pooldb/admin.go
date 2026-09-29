package pooldb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type GatewayKeyRow struct {
	APIKey    string    `json:"api_key"`
	Name      string    `json:"name"`
	Remark    string    `json:"remark"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	PoolSize  int       `json:"pool_size"`
}

type PoolAccountRow struct {
	Identifier        string    `json:"identifier"`
	HasPassword       bool      `json:"has_password"`
	TokenPreview      string    `json:"token_preview,omitempty"`
	Token             string    `json:"token,omitempty"`
	HasToken          bool      `json:"has_token"`
	HasDeviceID       bool      `json:"has_device_id"`
	DeviceID          string    `json:"device_id,omitempty"`
	DeviceIDPreview   string    `json:"device_id_preview,omitempty"`
	DeviceIDUpdatedAt time.Time `json:"device_id_updated_at,omitempty"`
	Position          int       `json:"position"`
	Discarded         bool      `json:"discarded"`
	DiscardReason     string    `json:"discard_reason,omitempty"`
	PoolStatusText    string    `json:"pool_status_text"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
	MuteUntil         time.Time `json:"mute_until,omitempty"`
}

func maskTokenPreview(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "…" + token[len(token)-4:]
}

func (db *DB) ListGatewayKeys(ctx context.Context) ([]GatewayKeyRow, error) {
	if err := db.configured(); err != nil {
		return nil, err
	}
	rows, err := db.sql.QueryContext(ctx, `
SELECT g.api_key, g.name, g.remark, g.enabled, g.created_at,
       COALESCE(SUM(CASE WHEN COALESCE(pa.discarded, 0) = 0 THEN 1 ELSE 0 END), 0) AS pool_size
FROM gateway_api_keys g
LEFT JOIN pool_bindings pb ON pb.api_key = g.api_key
LEFT JOIN pool_accounts pa ON pa.id = pb.account_id
GROUP BY g.api_key, g.name, g.remark, g.enabled, g.created_at
ORDER BY g.created_at ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatewayKeyRow
	for rows.Next() {
		var row GatewayKeyRow
		var enabled int
		var createdAt string
		if err := rows.Scan(&row.APIKey, &row.Name, &row.Remark, &enabled, &createdAt, &row.PoolSize); err != nil {
			return nil, err
		}
		row.Enabled = enabled != 0
		if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			row.CreatedAt = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", createdAt); err == nil {
			row.CreatedAt = t
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (db *DB) CreateGatewayKey(ctx context.Context, apiKey, name, remark string) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("api_key is required")
	}
	_, err := db.sql.ExecContext(ctx, `
INSERT INTO gateway_api_keys (api_key, name, remark) VALUES (?, ?, ?)
`, apiKey, strings.TrimSpace(name), strings.TrimSpace(remark))
	return err
}

func (db *DB) SetGatewayKeyEnabled(ctx context.Context, apiKey string, enabled bool) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	en := 0
	if enabled {
		en = 1
	}
	res, err := db.sql.ExecContext(ctx, `UPDATE gateway_api_keys SET enabled = ? WHERE api_key = ?`, en, apiKey)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("gateway api key not found")
	}
	return nil
}

func (db *DB) DeleteGatewayKey(ctx context.Context, apiKey string) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("api_key is required")
	}
	res, err := db.sql.ExecContext(ctx, `DELETE FROM gateway_api_keys WHERE api_key = ?`, apiKey)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("gateway api key not found")
	}
	return nil
}

func (db *DB) ListPoolAccounts(ctx context.Context, apiKey string) ([]PoolAccountRow, error) {
	if err := db.configured(); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	rows, err := db.sql.QueryContext(ctx, `
SELECT pa.identifier, pa.password, pa.token, pb.position, COALESCE(pa.discarded, 0), COALESCE(pa.discard_reason, ''), pa.created_at, COALESCE(pa.mute_until, ''), COALESCE(pa.device_id, ''), COALESCE(pa.device_id_updated_at, '')
FROM pool_bindings pb
INNER JOIN pool_accounts pa ON pa.id = pb.account_id
WHERE pb.api_key = ?
ORDER BY pb.position ASC, pa.id ASC
`, apiKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPoolAccountRows(rows)
}

func scanPoolAccountRows(rows *sql.Rows) ([]PoolAccountRow, error) {
	var out []PoolAccountRow
	for rows.Next() {
		var row PoolAccountRow
		var password, token, createdAt, muteUntil, deviceID, deviceUpdatedAt string
		var discarded int
		if err := rows.Scan(&row.Identifier, &password, &token, &row.Position, &discarded, &row.DiscardReason, &createdAt, &muteUntil, &deviceID, &deviceUpdatedAt); err != nil {
			return nil, err
		}
		row.Discarded = discarded != 0
		row.HasPassword = strings.TrimSpace(password) != ""
		row.HasToken = strings.TrimSpace(token) != ""
		row.TokenPreview = maskTokenPreview(token)
		if row.HasToken {
			row.Token = token
		}
		row.HasDeviceID = strings.TrimSpace(deviceID) != ""
		row.DeviceID = strings.TrimSpace(deviceID)
		row.DeviceIDPreview = maskTokenPreview(deviceID)
		row.PoolStatusText = PoolStatusLabel(row.Discarded, row.DiscardReason)
		if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			row.CreatedAt = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", createdAt); err == nil {
			row.CreatedAt = t
		}
		if t, err := time.Parse(time.RFC3339Nano, muteUntil); err == nil {
			row.MuteUntil = t
		} else if t, err := time.Parse(time.RFC3339, muteUntil); err == nil {
			row.MuteUntil = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", muteUntil); err == nil {
			row.MuteUntil = t
		}
		if t, err := time.Parse(time.RFC3339Nano, deviceUpdatedAt); err == nil {
			row.DeviceIDUpdatedAt = t
		} else if t, err := time.Parse(time.RFC3339, deviceUpdatedAt); err == nil {
			row.DeviceIDUpdatedAt = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", deviceUpdatedAt); err == nil {
			row.DeviceIDUpdatedAt = t
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (db *DB) nextBindingPosition(ctx context.Context, tx *sql.Tx, apiKey string) (int, error) {
	var pos int
	err := tx.QueryRowContext(ctx, `
SELECT COALESCE(MAX(position), -1) + 1 FROM pool_bindings WHERE api_key = ?
`, apiKey).Scan(&pos)
	return pos, err
}

// firstDeviceToken returns the first non-empty token from a value that may
// contain several device tokens separated by "|". The device-harvest CLI can
// emit multiple harvested tokens joined by "|"; a login only needs one, so the
// extras are dropped at import time.
func firstDeviceToken(deviceID string) string {
	for _, part := range strings.Split(deviceID, "|") {
		if part = strings.TrimSpace(part); part != "" {
			return part
		}
	}
	return ""
}

// AddAccountToPool upserts pool_accounts and binds it to the gateway key's pool.
// A non-empty deviceID refreshes the stored device token; an empty one keeps the existing value.
func (db *DB) AddAccountToPool(ctx context.Context, apiKey, identifier, password, deviceID string) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	identifier = strings.TrimSpace(identifier)
	deviceID = firstDeviceToken(deviceID)
	if apiKey == "" || identifier == "" {
		return fmt.Errorf("api_key and identifier are required")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var keyOK int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM gateway_api_keys WHERE api_key = ?`, apiKey).Scan(&keyOK); err != nil {
		return fmt.Errorf("gateway api key not found")
	}

	var deviceUpdatedAt any
	if deviceID != "" {
		deviceUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	var accountID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO pool_accounts (identifier, password, device_id, device_id_updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (identifier) DO UPDATE SET
  password = CASE WHEN excluded.password <> '' THEN excluded.password ELSE pool_accounts.password END,
  device_id = CASE WHEN excluded.device_id <> '' THEN excluded.device_id ELSE pool_accounts.device_id END,
  device_id_updated_at = CASE WHEN excluded.device_id <> '' THEN excluded.device_id_updated_at ELSE pool_accounts.device_id_updated_at END,
  discarded = 0
RETURNING id
`, identifier, password, deviceID, deviceUpdatedAt).Scan(&accountID)
	if err != nil {
		return err
	}

	var bound int
	_ = tx.QueryRowContext(ctx, `SELECT 1 FROM pool_bindings WHERE api_key = ? AND account_id = ?`, apiKey, accountID).Scan(&bound)
	if bound != 1 {
		pos, err := db.nextBindingPosition(ctx, tx, apiKey)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO pool_bindings (api_key, account_id, position) VALUES (?, ?, ?)
`, apiKey, accountID, pos); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveAccountFromPool removes binding; optionally marks account discarded when no bindings remain.
func (db *DB) RemoveAccountFromPool(ctx context.Context, apiKey, identifier string) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	identifier = strings.TrimSpace(identifier)
	res, err := db.sql.ExecContext(ctx, `
DELETE FROM pool_bindings
WHERE api_key = ? AND account_id IN (SELECT id FROM pool_accounts WHERE identifier = ?)
`, apiKey, identifier)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("account not in pool")
	}
	return nil
}
