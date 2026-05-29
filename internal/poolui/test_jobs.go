package poolui

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"whale2api/internal/pooldb"
)

var testJobLocks sync.Map // api_key -> *sync.Mutex

func (s *Server) testJobMutex(apiKey string) *sync.Mutex {
	v, _ := testJobLocks.LoadOrStore(apiKey, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (s *Server) getAccountTestJob(w http.ResponseWriter, r *http.Request) {
	apiKey := routeAPIKey(r)
	job, err := s.DB.GetAccountTestJob(r.Context(), apiKey)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jobToResponse(job))
}

func (s *Server) cancelAccountTestJob(w http.ResponseWriter, r *http.Request) {
	apiKey := routeAPIKey(r)
	ctx := r.Context()
	testRunnerFor(apiKey).stop()
	if err := s.DB.ResetAccountTestJob(ctx, apiKey); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "status": pooldb.TestJobStatusIdle})
}

func (s *Server) startAccountTestJob(w http.ResponseWriter, r *http.Request) {
	apiKey := routeAPIKey(r)
	var req accountTestRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	idents := req.identifiers()
	activeOnly := true
	if req.ActiveOnly != nil {
		activeOnly = *req.ActiveOnly
	}
	creds, err := s.DB.ListPoolAccountCredentials(r.Context(), apiKey, idents, activeOnly)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(creds) == 0 {
		writeJSON(w, http.StatusOK, accountTestResponse{Results: []accountTestResult{}})
		return
	}
	s.beginAccountTest(w, r, apiKey, creds)
}

func (s *Server) runPersistedAccountTests(apiKey string, creds []pooldb.PoolAccountCredential) {
	mu := s.testJobMutex(apiKey)
	mu.Lock()
	defer mu.Unlock()

	runner := testRunnerFor(apiKey)
	ctx := runner.start()
	defer runner.clear()

	res, cancelled := s.runAccountTestsWithProgress(ctx, apiKey, creds, func(done int, row accountTestResult, ok, failed, skipped int) {
		if s.isTestJobCancelled(apiKey) {
			return
		}
		_ = s.DB.UpdateAccountTestJobProgress(context.Background(), apiKey, accountTestResultToJob(row), done, ok, failed, skipped)
	})
	bg := context.Background()
	if cancelled || s.isTestJobCancelled(apiKey) {
		_ = s.DB.ResetAccountTestJob(bg, apiKey)
		return
	}
	job, err := s.DB.GetAccountTestJob(bg, apiKey)
	if err == nil && job.Status == pooldb.TestJobStatusCancelled {
		_ = s.DB.ResetAccountTestJob(bg, apiKey)
		return
	}
	_ = s.DB.FinishAccountTestJob(bg, apiKey, len(res.Results), res.OK, res.Failed, res.Skipped)
}

func accountTestResultToJob(row accountTestResult) pooldb.AccountTestJobResult {
	return pooldb.AccountTestJobResult{
		Identifier:    row.Identifier,
		OK:            row.OK,
		Message:       row.Message,
		TokenUpdated:  row.TokenUpdated,
		Skipped:       row.Skipped,
		PoolStatus:    row.PoolStatus,
		DiscardReason: row.DiscardReason,
		AutoDiscarded: row.AutoDiscarded,
	}
}

func jobToResponse(job pooldb.AccountTestJob) accountTestJobResponse {
	results := make([]accountTestResult, 0, len(job.Results))
	for _, r := range job.Results {
		results = append(results, accountTestResult{
			Identifier:    r.Identifier,
			OK:            r.OK,
			Message:       r.Message,
			TokenUpdated:  r.TokenUpdated,
			Skipped:       r.Skipped,
			PoolStatus:    r.PoolStatus,
			DiscardReason: r.DiscardReason,
			AutoDiscarded: r.AutoDiscarded,
		})
	}
	return accountTestJobResponse{
		APIKey:    job.APIKey,
		Status:    job.Status,
		Total:     job.Total,
		Done:      job.Done,
		OK:        job.OK,
		Failed:    job.Failed,
		Skipped:   job.Skipped,
		Results:   results,
		UpdatedAt: job.UpdatedAt,
	}
}

type accountTestJobResponse struct {
	APIKey    string              `json:"api_key"`
	Status    string              `json:"status"`
	Total     int                 `json:"total"`
	Done      int                 `json:"done"`
	OK        int                 `json:"ok"`
	Failed    int                 `json:"failed"`
	Skipped   int                 `json:"skipped"`
	Results   []accountTestResult `json:"results"`
	UpdatedAt any                 `json:"updated_at,omitempty"`
}
