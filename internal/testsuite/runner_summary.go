package testsuite

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (r *Runner) writeSummary(start, end time.Time) error {
	passed := 0
	failed := 0
	for _, cs := range r.results {
		if cs.Passed {
			passed++
		} else {
			failed++
		}
	}
	summary := runSummary{
		RunID:      r.runID,
		StartedAt:  start.Format(time.RFC3339Nano),
		EndedAt:    end.Format(time.RFC3339Nano),
		DurationMS: end.Sub(start).Milliseconds(),
		Stats: map[string]any{
			"total":  len(r.results),
			"passed": passed,
			"failed": failed,
		},
		Environment: map[string]any{
			"go_version":      runtime.Version(),
			"os":              runtime.GOOS,
			"arch":            runtime.GOARCH,
			"base_url":        r.baseURL,
			"config_fixture":  r.originalConfigPath,
			"config_via_env":  true,
			"server_log":      r.serverLog,
			"preflight_log":   r.preflightLog,
			"retries":         r.opts.Retries,
			"timeout_seconds": int(r.opts.Timeout.Seconds()),
		},
		Cases:    r.results,
		Warnings: r.warnings,
	}
	if err := writeJSONFile(filepath.Join(r.runDir, "summary.json"), summary); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.runDir, "summary.md"), []byte(r.summaryMarkdown(summary)), 0o644)
}

func (r *Runner) summaryMarkdown(s runSummary) string {
	var b strings.Builder
	b.WriteString("# Whale2API Live Testsuite Summary\n\n")
	b.WriteString("**Sensitive Notice:** this run stores full raw request/response logs. Do not share artifacts publicly.\n\n")
	fmt.Fprintf(&b, "- Run ID: `%s`\n", s.RunID)
	fmt.Fprintf(&b, "- Started: `%s`\n", s.StartedAt)
	fmt.Fprintf(&b, "- Ended: `%s`\n", s.EndedAt)
	fmt.Fprintf(&b, "- Duration: `%d ms`\n", s.DurationMS)
	fmt.Fprintf(&b, "- Passed/Failed: `%d/%d`\n\n", s.Stats["passed"], s.Stats["failed"])
	if len(s.Warnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, w := range s.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
		b.WriteString("\n")
	}
	b.WriteString("## Failed Cases\n\n")
	hasFailed := false
	for _, c := range s.Cases {
		if c.Passed {
			continue
		}
		hasFailed = true
		fmt.Fprintf(&b, "- `%s`: %s\n", c.CaseID, c.Error)
		if len(c.TraceIDs) > 0 {
			fmt.Fprintf(&b, "  - trace_ids: `%s`\n", strings.Join(c.TraceIDs, ", "))
			fmt.Fprintf(&b, "  - grep: `rg \"%s\" %s`\n", c.TraceIDs[0], filepath.Join(r.runDir, "server.log"))
		}
		fmt.Fprintf(&b, "  - artifact: `%s`\n", c.ArtifactPath)
	}
	if !hasFailed {
		b.WriteString("- none\n")
	}
	b.WriteString("\n## Case Table\n\n")
	b.WriteString("| case_id | status | duration_ms | statuses | artifact |\n")
	b.WriteString("|---|---:|---:|---|---|\n")
	for _, c := range s.Cases {
		status := "PASS"
		if !c.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "| %s | %s | %d | %v | `%s` |\n", c.CaseID, status, c.DurationMS, c.StatusCodes, c.ArtifactPath)
	}
	return b.String()
}
