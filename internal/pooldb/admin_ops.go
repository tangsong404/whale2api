package pooldb

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type ImportResult struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors,omitempty"`
}

// RotateGatewayAPIKey renames a gateway key; pool bindings move with it.
func (db *DB) RotateGatewayAPIKey(ctx context.Context, oldKey, newKey string) error {
	if err := db.configured(); err != nil {
		return err
	}
	oldKey = strings.TrimSpace(oldKey)
	newKey = strings.TrimSpace(newKey)
	if oldKey == "" || newKey == "" {
		return fmt.Errorf("old_key and new_key are required")
	}
	if oldKey == newKey {
		return nil
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var name, remark string
	var enabled int
	err = tx.QueryRowContext(ctx, `SELECT name, remark, enabled FROM gateway_api_keys WHERE api_key = ?`, oldKey).Scan(&name, &remark, &enabled)
	if err != nil {
		return fmt.Errorf("gateway api key not found")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO gateway_api_keys (api_key, name, remark, enabled) VALUES (?, ?, ?, ?)`, newKey, name, remark, enabled); err != nil {
		if IsUniqueViolation(err) {
			return fmt.Errorf("new api_key already exists")
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pool_bindings SET api_key = ? WHERE api_key = ?`, newKey, oldKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM gateway_api_keys WHERE api_key = ?`, oldKey); err != nil {
		return err
	}
	return tx.Commit()
}

// SetAccountDiscarded marks an account in a pool as discarded (still bound, not used for traffic).
func (db *DB) SetAccountDiscarded(ctx context.Context, apiKey, identifier string, discarded bool) error {
	reason := DiscardReasonManual
	if !discarded {
		reason = DiscardReasonNone
	}
	return db.SetAccountPoolState(ctx, apiKey, identifier, discarded, reason)
}

// SetAccountPoolState updates discarded flag and reason (muted / banned / manual).
func (db *DB) SetAccountPoolState(ctx context.Context, apiKey, identifier string, discarded bool, reason string) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	identifier = strings.TrimSpace(identifier)
	reason = strings.TrimSpace(reason)
	disc := 0
	if discarded {
		disc = 1
		if reason == "" {
			reason = DiscardReasonManual
		}
	} else {
		reason = DiscardReasonNone
	}
	res, err := db.sql.ExecContext(ctx, `
UPDATE pool_accounts
SET discarded = ?, discard_reason = ?
WHERE id IN (
  SELECT pa.id FROM pool_accounts pa
  INNER JOIN pool_bindings pb ON pb.account_id = pa.id
  WHERE pb.api_key = ? AND pa.identifier = ?
)
`, disc, reason, apiKey, identifier)
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

func (db *DB) ListPoolAccountsAll(ctx context.Context, apiKey string, includeDiscarded bool) ([]PoolAccountRow, error) {
	if err := db.configured(); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	q := `
SELECT pa.identifier, pa.password, pa.token, pb.position, COALESCE(pa.discarded, 0), COALESCE(pa.discard_reason, '')
FROM pool_bindings pb
INNER JOIN pool_accounts pa ON pa.id = pb.account_id
WHERE pb.api_key = ?`
	if !includeDiscarded {
		q += ` AND COALESCE(pa.discarded, 0) = 0`
	}
	q += ` ORDER BY pb.position ASC, pa.id ASC`
	rows, err := db.sql.QueryContext(ctx, q, apiKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPoolAccountRows(rows)
}

// ImportAccountsCSV imports rows with columns identifier/email/mobile and password.
func (db *DB) ImportAccountsCSV(ctx context.Context, apiKey string, r io.Reader) (ImportResult, error) {
	if err := db.configured(); err != nil {
		return ImportResult{}, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ImportResult{}, fmt.Errorf("api_key is required")
	}
	var keyOK int
	if err := db.sql.QueryRowContext(ctx, `SELECT 1 FROM gateway_api_keys WHERE api_key = ?`, apiKey).Scan(&keyOK); err != nil {
		return ImportResult{}, fmt.Errorf("gateway api key not found")
	}

	raw, err := io.ReadAll(r)
	if err != nil {
		return ImportResult{}, fmt.Errorf("read csv: %w", err)
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(raw) {
		return ImportResult{}, fmt.Errorf("csv must be UTF-8 encoded")
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return ImportResult{}, fmt.Errorf("parse csv: %w", err)
	}
	if len(records) == 0 {
		return ImportResult{}, fmt.Errorf("empty csv")
	}

	header := records[0]
	start := 0
	idCol, passCol := 0, 1
	if looksLikeHeader(header) {
		idCol, passCol = mapCSVColumns(header)
		start = 1
	}
	if passCol < 0 {
		return ImportResult{}, fmt.Errorf("csv must include password column")
	}

	var res ImportResult
	for i := start; i < len(records); i++ {
		row := records[i]
		if len(row) == 0 || rowIsEmpty(row) {
			res.Skipped++
			continue
		}
		ident := ""
		if idCol >= 0 && idCol < len(row) {
			ident = strings.TrimSpace(row[idCol])
		}
		if ident == "" {
			for _, cell := range row {
				cell = strings.TrimSpace(cell)
				if strings.Contains(cell, "@") {
					ident = cell
					break
				}
			}
		}
		if ident == "" && idCol < len(row) {
			ident = strings.TrimSpace(row[0])
		}
		pass := ""
		if passCol < len(row) {
			pass = strings.TrimSpace(row[passCol])
		}
		if ident == "" {
			res.Skipped++
			res.Errors = append(res.Errors, fmt.Sprintf("row %d: missing identifier", i+1))
			continue
		}
		if err := db.AddAccountToPool(ctx, apiKey, ident, pass); err != nil {
			if IsUniqueViolation(err) {
				_ = db.SetAccountDiscarded(ctx, apiKey, ident, false)
				res.Skipped++
				continue
			}
			res.Errors = append(res.Errors, fmt.Sprintf("row %d: %v", i+1, err))
			continue
		}
		res.Imported++
	}
	return res, nil
}

func looksLikeHeader(row []string) bool {
	if len(row) == 0 {
		return false
	}
	joined := strings.ToLower(strings.Join(row, ","))
	return strings.Contains(joined, "email") || strings.Contains(joined, "identifier") ||
		strings.Contains(joined, "password") || strings.Contains(joined, "mobile")
}

func mapCSVColumns(header []string) (idCol, passCol int) {
	idCol, passCol = 0, 1
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(h))
		switch {
		case h == "email" || h == "identifier" || h == "mobile" || h == "account":
			idCol = i
		case h == "password" || h == "pass" || h == "pwd":
			passCol = i
		}
	}
	return idCol, passCol
}

func rowIsEmpty(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// UpdateGatewayKeyMeta updates name/remark/enabled without changing the key string.
func (db *DB) UpdateGatewayKeyMeta(ctx context.Context, apiKey, name, remark string, enabled *bool) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	if enabled != nil {
		en := 0
		if *enabled {
			en = 1
		}
		res, err := db.sql.ExecContext(ctx, `UPDATE gateway_api_keys SET name = ?, remark = ?, enabled = ? WHERE api_key = ?`,
			strings.TrimSpace(name), strings.TrimSpace(remark), en, apiKey)
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
	res, err := db.sql.ExecContext(ctx, `UPDATE gateway_api_keys SET name = ?, remark = ? WHERE api_key = ?`,
		strings.TrimSpace(name), strings.TrimSpace(remark), apiKey)
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
