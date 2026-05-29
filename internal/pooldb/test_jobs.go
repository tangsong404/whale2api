package pooldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TestJobStatusIdle      = "idle"
	TestJobStatusRunning   = "running"
	TestJobStatusCompleted = "completed"
	TestJobStatusCancelled = "cancelled"
)

// AccountTestJobResult is one account row in a persisted test job.
type AccountTestJobResult struct {
	Identifier    string `json:"identifier"`
	OK            bool   `json:"ok"`
	Message       string `json:"message,omitempty"`
	TokenUpdated  bool   `json:"token_updated"`
	Skipped       bool   `json:"skipped,omitempty"`
	PoolStatus    string `json:"pool_status,omitempty"`
	DiscardReason string `json:"discard_reason,omitempty"`
	AutoDiscarded bool   `json:"auto_discarded,omitempty"`
}

// AccountTestJob is the persisted batch test state for one gateway key.
type AccountTestJob struct {
	APIKey    string                 `json:"api_key"`
	Status    string                 `json:"status"`
	Total     int                    `json:"total"`
	Done      int                    `json:"done"`
	OK        int                    `json:"ok"`
	Failed    int                    `json:"failed"`
	Skipped   int                    `json:"skipped"`
	Results   []AccountTestJobResult `json:"results"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// GetAccountTestJob returns the stored test job for a gateway key (idle when missing).
func (db *DB) GetAccountTestJob(ctx context.Context, apiKey string) (AccountTestJob, error) {
	if err := db.configured(); err != nil {
		return AccountTestJob{}, err
	}
	apiKey = strings.TrimSpace(apiKey)
	job := AccountTestJob{APIKey: apiKey, Status: TestJobStatusIdle, Results: []AccountTestJobResult{}}
	var raw string
	var updatedAt string
	err := db.sql.QueryRowContext(ctx, `
		SELECT status, total, done, ok_count, failed_count, skipped_count, results, updated_at
		FROM pool_account_test_jobs WHERE api_key = ?`, apiKey,
	).Scan(&job.Status, &job.Total, &job.Done, &job.OK, &job.Failed, &job.Skipped, &raw, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return job, nil
		}
		return job, err
	}
	if updatedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
			job.UpdatedAt = t
		} else if t, err := time.Parse("2006-01-02 15:04:05", updatedAt); err == nil {
			job.UpdatedAt = t
		}
	}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &job.Results)
	}
	if job.Results == nil {
		job.Results = []AccountTestJobResult{}
	}
	return job, nil
}

// StartAccountTestJob resets and marks a test job as running.
func (db *DB) StartAccountTestJob(ctx context.Context, apiKey string, total int) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("api_key is required")
	}
	if total < 0 {
		total = 0
	}
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO pool_account_test_jobs (api_key, status, total, done, ok_count, failed_count, skipped_count, results, updated_at)
		VALUES (?, ?, ?, 0, 0, 0, 0, '[]', datetime('now'))
		ON CONFLICT (api_key) DO UPDATE SET
			status = excluded.status,
			total = excluded.total,
			done = 0,
			ok_count = 0,
			failed_count = 0,
			skipped_count = 0,
			results = '[]',
			updated_at = datetime('now')`, apiKey, TestJobStatusRunning, total)
	return err
}

// UpdateAccountTestJobProgress appends one result and updates counters.
func (db *DB) UpdateAccountTestJobProgress(ctx context.Context, apiKey string, row AccountTestJobResult, done, ok, failed, skipped int) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var raw string
	err = tx.QueryRowContext(ctx, `
		SELECT results FROM pool_account_test_jobs WHERE api_key = ? AND status = ?`,
		apiKey, TestJobStatusRunning).Scan(&raw)
	if err != nil {
		return err
	}
	var results []AccountTestJobResult
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &results); err != nil {
			return err
		}
	}
	results = append(results, row)
	merged, err := json.Marshal(results)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE pool_account_test_jobs SET
			done = ?,
			ok_count = ?,
			failed_count = ?,
			skipped_count = ?,
			results = ?,
			updated_at = datetime('now')
		WHERE api_key = ? AND status = ?`,
		done, ok, failed, skipped, string(merged), apiKey, TestJobStatusRunning)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// FinishAccountTestJob marks the job completed.
func (db *DB) FinishAccountTestJob(ctx context.Context, apiKey string, done, ok, failed, skipped int) error {
	return db.finishAccountTestJob(ctx, apiKey, TestJobStatusCompleted, done, ok, failed, skipped)
}

// CancelAccountTestJob marks the job stopped by user; keeps progress done so far.
func (db *DB) CancelAccountTestJob(ctx context.Context, apiKey string, done, ok, failed, skipped int) error {
	return db.finishAccountTestJob(ctx, apiKey, TestJobStatusCancelled, done, ok, failed, skipped)
}

// MarkAccountTestJobCancelled sets status to cancelled while a job is still running (user stop).
func (db *DB) MarkAccountTestJobCancelled(ctx context.Context, apiKey string) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	_, err := db.sql.ExecContext(ctx, `
		UPDATE pool_account_test_jobs SET status = ?, updated_at = datetime('now')
		WHERE api_key = ? AND status = ?`, TestJobStatusCancelled, apiKey, TestJobStatusRunning)
	return err
}

// ResetAllRunningAccountTestJobs clears jobs left "running" after pool UI restart.
func (db *DB) ResetAllRunningAccountTestJobs(ctx context.Context) (int64, error) {
	if err := db.configured(); err != nil {
		return 0, err
	}
	res, err := db.sql.ExecContext(ctx, `
		UPDATE pool_account_test_jobs SET
			status = ?,
			total = 0,
			done = 0,
			ok_count = 0,
			failed_count = 0,
			skipped_count = 0,
			results = '[]',
			updated_at = datetime('now')
		WHERE status = ?`, TestJobStatusIdle, TestJobStatusRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ResetAccountTestJob clears persisted test progress (after user abort).
func (db *DB) ResetAccountTestJob(ctx context.Context, apiKey string) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	_, err := db.sql.ExecContext(ctx, `
		INSERT INTO pool_account_test_jobs (api_key, status, total, done, ok_count, failed_count, skipped_count, results, updated_at)
		VALUES (?, ?, 0, 0, 0, 0, 0, '[]', datetime('now'))
		ON CONFLICT (api_key) DO UPDATE SET
			status = excluded.status,
			total = 0,
			done = 0,
			ok_count = 0,
			failed_count = 0,
			skipped_count = 0,
			results = '[]',
			updated_at = datetime('now')`, apiKey, TestJobStatusIdle)
	return err
}

func (db *DB) finishAccountTestJob(ctx context.Context, apiKey, status string, done, ok, failed, skipped int) error {
	if err := db.configured(); err != nil {
		return err
	}
	apiKey = strings.TrimSpace(apiKey)
	_, err := db.sql.ExecContext(ctx, `
		UPDATE pool_account_test_jobs SET
			status = ?,
			ok_count = ?,
			failed_count = ?,
			skipped_count = ?,
			done = ?,
			updated_at = datetime('now')
		WHERE api_key = ?`, status, ok, failed, skipped, done, apiKey)
	return err
}
