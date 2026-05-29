package poolui

import (
	"context"
	"strings"

	"whale2api/internal/accountprobe"
	dsclient "whale2api/internal/deepseek/client"
	"whale2api/internal/pooldb"
)

type accountTestResult struct {
	Identifier    string `json:"identifier"`
	OK            bool   `json:"ok"`
	Message       string `json:"message,omitempty"`
	TokenUpdated  bool   `json:"token_updated"`
	Skipped       bool   `json:"skipped,omitempty"`
	PoolStatus    string `json:"pool_status,omitempty"`
	DiscardReason string `json:"discard_reason,omitempty"`
	AutoDiscarded bool   `json:"auto_discarded,omitempty"`
}

type accountTestResponse struct {
	Total     int                 `json:"total"`
	OK        int                 `json:"ok"`
	Failed    int                 `json:"failed"`
	Skipped   int                 `json:"skipped"`
	Cancelled bool                `json:"cancelled,omitempty"`
	Results   []accountTestResult `json:"results"`
}

func (s *Server) runAccountTests(ctx context.Context, apiKey string, creds []pooldb.PoolAccountCredential) accountTestResponse {
	res, _ := s.runAccountTestsWithProgress(ctx, apiKey, creds, nil)
	return res
}

func (s *Server) probeOneAccount(ctx context.Context, apiKey string, cred pooldb.PoolAccountCredential) accountTestResult {
	ds := dsclient.NewClient(nil, nil)
	row := accountTestResult{Identifier: cred.Identifier}
	if strings.TrimSpace(cred.Password) == "" {
		row.Skipped = true
		row.Message = "缺少密码"
		return row
	}

	acc := pooldb.AccountToConfig(cred.Identifier, cred.Password)
	probe := accountprobe.Probe(ctx, ds, acc, accountprobe.DefaultProbePrompt)
	row.PoolStatus = probe.PoolStatus
	row.DiscardReason = probe.DiscardReason

	if probe.Token != "" {
		if err := s.DB.UpdateAccountToken(ctx, cred.Identifier, probe.Token); err == nil {
			row.TokenUpdated = true
		}
	}

	if probe.AutoDiscard && probe.DiscardReason != "" {
		if err := s.DB.SetAccountPoolState(ctx, apiKey, cred.Identifier, true, probe.DiscardReason); err == nil {
			row.AutoDiscarded = true
		}
	}

	if probe.OK {
		row.OK = true
		row.Message = probe.Message
		_ = s.DB.SetAccountPoolState(ctx, apiKey, cred.Identifier, false, pooldb.DiscardReasonNone)
		return row
	}

	row.OK = false
	row.Message = probe.Message
	return row
}

func (s *Server) runAccountTestsWithProgress(
	ctx context.Context,
	apiKey string,
	creds []pooldb.PoolAccountCredential,
	onProgress func(done int, row accountTestResult, ok, failed, skipped int),
) (accountTestResponse, bool) {
	results := make([]accountTestResult, 0, len(creds))
	var okCount, failedCount, skippedCount int

	for _, cred := range creds {
		if err := ctx.Err(); err != nil {
			break
		}
		if s.isTestJobCancelled(apiKey) {
			break
		}
		row := s.probeOneAccount(ctx, apiKey, cred)
		results = append(results, row)
		switch {
		case row.Skipped:
			skippedCount++
		case row.OK:
			okCount++
		default:
			failedCount++
		}
		if onProgress != nil {
			onProgress(len(results), row, okCount, failedCount, skippedCount)
		}
	}

	cancelled := ctx.Err() != nil || s.isTestJobCancelled(apiKey)
	return accountTestResponse{
		Total:     len(creds),
		OK:        okCount,
		Failed:    failedCount,
		Skipped:   skippedCount,
		Cancelled: cancelled,
		Results:   results,
	}, cancelled
}
