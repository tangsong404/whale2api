package testsuite

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Options struct {
	ConfigPath  string
	OutputDir   string
	Port        int
	Timeout     time.Duration
	Retries     int
	NoPreflight bool
	MaxKeepRuns int
}

type runSummary struct {
	RunID       string         `json:"run_id"`
	StartedAt   string         `json:"started_at"`
	EndedAt     string         `json:"ended_at"`
	DurationMS  int64          `json:"duration_ms"`
	Stats       map[string]any `json:"stats"`
	Environment map[string]any `json:"environment"`
	Cases       []caseResult   `json:"cases"`
	Warnings    []string       `json:"warnings,omitempty"`
}

type caseResult struct {
	CaseID       string            `json:"case_id"`
	Passed       bool              `json:"passed"`
	DurationMS   int64             `json:"duration_ms"`
	TraceIDs     []string          `json:"trace_ids"`
	StatusCodes  []int             `json:"status_codes"`
	Error        string            `json:"error,omitempty"`
	ArtifactPath string            `json:"artifact_path"`
	Assertions   []assertionResult `json:"assertions"`
}

type assertionResult struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type requestLog struct {
	Seq       int               `json:"seq"`
	Attempt   int               `json:"attempt"`
	TraceID   string            `json:"trace_id"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Body      any               `json:"body,omitempty"`
	Timestamp string            `json:"timestamp"`
}

type responseLog struct {
	Seq        int                 `json:"seq"`
	Attempt    int                 `json:"attempt"`
	TraceID    string              `json:"trace_id"`
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	BodyText   string              `json:"body_text"`
	DurationMS int64               `json:"duration_ms"`
	NetworkErr string              `json:"network_error,omitempty"`
	ReceivedAt string              `json:"received_at"`
}

type caseContext struct {
	runner      *Runner
	id          string
	dir         string
	startedAt   time.Time
	mu          sync.Mutex
	seq         int
	assertions  []assertionResult
	requests    []requestLog
	responses   []responseLog
	streamRaw   strings.Builder
	traceIDsSet map[string]struct{}
}

type requestSpec struct {
	Method    string
	Path      string
	Headers   map[string]string
	Body      any
	Stream    bool
	Retryable bool
}

type responseResult struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	TraceID    string
	URL        string
}

type Runner struct {
	opts Options

	runID        string
	runDir       string
	serverLog    string
	preflightLog string

	baseURL     string
	httpClient  *http.Client
	serverCmd   *exec.Cmd
	serverLogFd *os.File

	originalConfigPath string
	originalConfigHash string

	configRaw runConfig
	apiKey    string
	accountID string

	warnings []string
	results  []caseResult
}

type runConfig struct {
	Keys     []string `json:"keys"`
	Accounts []struct {
		Email    string `json:"email,omitempty"`
		Mobile   string `json:"mobile,omitempty"`
		Password string `json:"password,omitempty"`
		Token    string `json:"token,omitempty"`
	} `json:"accounts"`
}

func Run(ctx context.Context, opts Options) error {
	r, err := newRunner(opts)
	if err != nil {
		return err
	}
	start := time.Now()
	defer func() {
		_ = r.stopServer()
	}()

	if err := r.prepareRunDir(); err != nil {
		return err
	}

	if !r.opts.NoPreflight {
		if err := r.runPreflight(ctx); err != nil {
			_ = r.writeSummary(start, time.Now())
			return err
		}
	}

	if err := r.preparePoolFixture(); err != nil {
		_ = r.writeSummary(start, time.Now())
		return err
	}

	if err := r.seedGatewayPool(ctx); err != nil {
		_ = r.writeSummary(start, time.Now())
		return err
	}

	if err := r.startServer(ctx); err != nil {
		_ = r.writeSummary(start, time.Now())
		return err
	}

	for _, c := range r.cases() {
		r.runCase(ctx, c)
	}

	if err := r.ensureOriginalConfigUntouched(); err != nil {
		r.warnings = append(r.warnings, err.Error())
	}

	end := time.Now()
	if err := r.writeSummary(start, end); err != nil {
		return err
	}

	// Prune old test runs, keeping only the most recent N.
	if err := r.pruneOldRuns(); err != nil {
		r.warnings = append(r.warnings, "prune old runs: "+err.Error())
	}

	failed := 0
	for _, cs := range r.results {
		if !cs.Passed {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("testsuite failed: %d case(s) failed, see %s", failed, filepath.Join(r.runDir, "summary.md"))
	}
	return nil
}

func newRunner(opts Options) (*Runner, error) {
	if strings.TrimSpace(opts.ConfigPath) == "" {
		opts.ConfigPath = "tests/fixtures/gateway-pool.json"
	}
	if strings.TrimSpace(opts.OutputDir) == "" {
		opts.OutputDir = "artifacts/testsuite"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 120 * time.Second
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	}
	return &Runner{
		opts: opts,
		httpClient: &http.Client{
			Timeout: 0,
		},
		runID: time.Now().UTC().Format("20060102T150405Z"),
	}, nil
}
func (r *Runner) runCase(ctx context.Context, c caseDef) {
	caseDir := filepath.Join(r.runDir, "cases", c.ID)
	_ = os.MkdirAll(caseDir, 0o755)
	cc := &caseContext{
		runner:      r,
		id:          c.ID,
		dir:         caseDir,
		startedAt:   time.Now(),
		traceIDsSet: map[string]struct{}{},
	}
	err := c.Run(ctx, cc)
	duration := time.Since(cc.startedAt).Milliseconds()

	if err != nil {
		cc.assertions = append(cc.assertions, assertionResult{
			Name:   "case_error",
			Passed: false,
			Detail: err.Error(),
		})
	}
	passed := err == nil
	for _, a := range cc.assertions {
		if !a.Passed {
			passed = false
			break
		}
	}

	traceIDs := make([]string, 0, len(cc.traceIDsSet))
	for t := range cc.traceIDsSet {
		traceIDs = append(traceIDs, t)
	}
	sort.Strings(traceIDs)
	statuses := uniqueStatusCodes(cc.responses)
	cs := caseResult{
		CaseID:       c.ID,
		Passed:       passed,
		DurationMS:   duration,
		TraceIDs:     traceIDs,
		StatusCodes:  statuses,
		ArtifactPath: caseDir,
		Assertions:   cc.assertions,
	}
	if err != nil {
		cs.Error = err.Error()
	}
	_ = cc.flushArtifacts(cs)
	r.results = append(r.results, cs)
}
