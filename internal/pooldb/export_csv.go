package pooldb

import (
	"bytes"
	"context"
	"encoding/csv"
	"strings"
)

// PoolAccountExportRow is one account bound to a gateway key for CSV export.
type PoolAccountExportRow struct {
	Identifier string
	Password   string
	Discarded  bool
}

// ListPoolAccountsForExport returns bound accounts with passwords for admin export.
func (db *DB) ListPoolAccountsForExport(ctx context.Context, apiKey string, includeDiscarded bool) ([]PoolAccountExportRow, error) {
	if err := db.configured(); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	q := `
SELECT pa.identifier, pa.password, COALESCE(pa.discarded, 0)
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
	var out []PoolAccountExportRow
	for rows.Next() {
		var row PoolAccountExportRow
		var discarded int
		if err := rows.Scan(&row.Identifier, &row.Password, &discarded); err != nil {
			return nil, err
		}
		row.Discarded = discarded != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

// ExportAccountsCSV builds UTF-8 CSV (with BOM) compatible with ImportAccountsCSV.
func (db *DB) ExportAccountsCSV(ctx context.Context, apiKey string, includeDiscarded bool) ([]byte, error) {
	rows, err := db.ListPoolAccountsForExport(ctx, apiKey, includeDiscarded)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"email", "password", "discarded"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		disc := "false"
		if row.Discarded {
			disc = "true"
		}
		if err := w.Write([]string{row.Identifier, row.Password, disc}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
