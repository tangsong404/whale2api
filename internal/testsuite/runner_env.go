package testsuite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"whale2api/internal/config"
	"whale2api/internal/pooldb"
)

func (r *Runner) prepareRunDir() error {
	r.runDir = filepath.Join(r.opts.OutputDir, r.runID)
	if err := os.MkdirAll(r.runDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(r.runDir, "cases"), 0o755); err != nil {
		return err
	}
	r.serverLog = filepath.Join(r.runDir, "server.log")
	r.preflightLog = filepath.Join(r.runDir, "preflight.log")
	return nil
}

// pruneOldRuns removes old test run directories, keeping the most recent MaxKeepRuns.
// Run IDs use the format "20060102T150405Z", so alphabetical order == chronological order.
func (r *Runner) pruneOldRuns() error {
	keep := r.opts.MaxKeepRuns
	if keep <= 0 {
		return nil // 0 or negative means no pruning
	}

	entries, err := os.ReadDir(r.opts.OutputDir)
	if err != nil {
		return err
	}

	// Collect only directories (each run is a directory).
	var runDirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		runDirs = append(runDirs, e.Name())
	}

	sort.Strings(runDirs)

	if len(runDirs) <= keep {
		return nil
	}

	// Remove oldest runs (those at the beginning of the sorted list).
	toRemove := runDirs[:len(runDirs)-keep]
	var errs []string
	for _, name := range toRemove {
		dirPath := filepath.Join(r.opts.OutputDir, name)
		if err := os.RemoveAll(dirPath); err != nil {
			errs = append(errs, fmt.Sprintf("remove %s: %v", name, err))
		} else {
			_, _ = fmt.Fprintf(os.Stdout, "pruned old test run: %s\n", name)
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (r *Runner) runPreflight(ctx context.Context) error {
	steps := preflightSteps()
	f, err := os.OpenFile(r.preflightLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	for _, step := range steps {
		if _, err := fmt.Fprintf(f, "\n$ %s\n", strings.Join(step, " ")); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, step[0], step[1:]...)
		cmd.Stdout = f
		cmd.Stderr = f
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("preflight failed at `%s`: %w", strings.Join(step, " "), err)
		}
	}
	return nil
}

func preflightSteps() [][]string {
	return [][]string{
		{"go", "test", "./...", "-count=1"},
		{"./tests/scripts/check-node-split-syntax.sh"},
		{"node", "--test", "tests/node/stream-tool-sieve.test.js", "tests/node/chat-stream.test.js", "tests/node/js_compat_test.js"},
	}
}

func (r *Runner) preparePoolFixture() error {
	abs, err := filepath.Abs(r.opts.ConfigPath)
	if err != nil {
		return err
	}
	r.originalConfigPath = abs
	raw, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	r.originalConfigHash = hex.EncodeToString(sum[:])

	tmpDir := filepath.Join(r.runDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	var cfg runConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("parse config failed: %w", err)
	}
	r.configRaw = cfg
	if len(cfg.Keys) > 0 {
		r.apiKey = strings.TrimSpace(cfg.Keys[0])
	}
	for _, acc := range cfg.Accounts {
		id := strings.TrimSpace(acc.Email)
		if id == "" {
			id = strings.TrimSpace(acc.Mobile)
		}
		if id != "" {
			r.accountID = id
			break
		}
	}
	return nil
}

func (r *Runner) seedGatewayPool(ctx context.Context) error {
	path := strings.TrimSpace(os.Getenv("WHALE2API_DATABASE_PATH"))
	if path == "" {
		path = pooldb.DefaultDatabasePath
	}
	db, err := pooldb.Connect(ctx, path)
	if err != nil {
		return fmt.Errorf("connect pool database: %w", err)
	}
	defer db.Close()

	accounts := make([]config.Account, 0, len(r.configRaw.Accounts))
	for _, a := range r.configRaw.Accounts {
		accounts = append(accounts, config.Account{
			Email:    a.Email,
			Mobile:   a.Mobile,
			Password: a.Password,
			Token:    a.Token,
		})
	}
	for _, key := range r.configRaw.Keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if err := db.SeedGatewayPool(ctx, key, accounts); err != nil {
			return fmt.Errorf("seed gateway key %q: %w", key, err)
		}
	}
	return nil
}

func (r *Runner) startServer(ctx context.Context) error {
	port := r.opts.Port
	if port <= 0 {
		p, err := findFreePort()
		if err != nil {
			return err
		}
		port = p
	}
	r.baseURL = "http://127.0.0.1:" + strconv.Itoa(port)

	logFd, err := os.OpenFile(r.serverLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	r.serverLogFd = logFd
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/whale2api")
	cmd.Stdout = logFd
	cmd.Stderr = logFd
	dbPath := strings.TrimSpace(os.Getenv("WHALE2API_DATABASE_PATH"))
	if dbPath == "" {
		dbPath = pooldb.DefaultDatabasePath
	}
	cmd.Env = prepareServerEnv(os.Environ(), map[string]string{
		"PORT":                       strconv.Itoa(port),
		"WHALE2API_AUTO_BUILD_WEBUI": "false",
		"WHALE2API_DATABASE_PATH":    dbPath,
	})
	if err := cmd.Start(); err != nil {
		_ = logFd.Close()
		return err
	}
	r.serverCmd = cmd

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if r.ping("/healthz") == nil && r.ping("/readyz") == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("server readiness timeout")
}

func (r *Runner) stopServer() error {
	var errs []string
	if r.serverCmd != nil && r.serverCmd.Process != nil {
		_ = r.serverCmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- r.serverCmd.Wait() }()
		select {
		case <-time.After(5 * time.Second):
			_ = r.serverCmd.Process.Kill()
			<-done
		case <-done:
		}
	}
	if r.serverLogFd != nil {
		if err := r.serverLogFd.Close(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (r *Runner) ping(path string) error {
	resp, err := r.httpClient.Get(r.baseURL + path)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status=%d", resp.StatusCode)
	}
	return nil
}

func (r *Runner) ensureOriginalConfigUntouched() error {
	raw, err := os.ReadFile(r.originalConfigPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	current := hex.EncodeToString(sum[:])
	if current != r.originalConfigHash {
		return fmt.Errorf("original config changed unexpectedly: %s", r.originalConfigPath)
	}
	return nil
}
