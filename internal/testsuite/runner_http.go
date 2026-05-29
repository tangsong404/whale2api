package testsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (cc *caseContext) assert(name string, ok bool, detail string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.assertions = append(cc.assertions, assertionResult{
		Name:   name,
		Passed: ok,
		Detail: detail,
	})
}

func (cc *caseContext) request(ctx context.Context, spec requestSpec) (*responseResult, error) {
	retries := cc.runner.opts.Retries
	if !spec.Retryable {
		retries = 0
	}
	var lastErr error
	for attempt := 1; attempt <= retries+1; attempt++ {
		resp, err := cc.requestOnce(ctx, spec, attempt)
		if err == nil && resp.StatusCode < 500 {
			return resp, nil
		}
		if err != nil {
			lastErr = err
		} else if resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("status=%d", resp.StatusCode)
		}
		if attempt <= retries {
			sleep := time.Duration(300*(1<<(attempt-1))) * time.Millisecond
			time.Sleep(sleep)
		}
	}
	return nil, lastErr
}

func (cc *caseContext) requestOnce(ctx context.Context, spec requestSpec, attempt int) (*responseResult, error) {
	cc.mu.Lock()
	cc.seq++
	seq := cc.seq
	traceID := fmt.Sprintf("ts_%s_%s_%03d", cc.runner.runID, sanitizeID(cc.id), seq)
	cc.traceIDsSet[traceID] = struct{}{}
	cc.mu.Unlock()

	fullURL, err := withTraceQuery(cc.runner.baseURL+spec.Path, traceID)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{}
	for k, v := range spec.Headers {
		headers[k] = v
	}
	headers["X-Whale2-Test-Trace"] = traceID

	var bodyBytes []byte
	var bodyAny any
	if spec.Body != nil {
		b, err := json.Marshal(spec.Body)
		if err != nil {
			return nil, err
		}
		bodyBytes = b
		bodyAny = spec.Body
		headers["Content-Type"] = "application/json"
	}
	cc.mu.Lock()
	cc.requests = append(cc.requests, requestLog{
		Seq:       seq,
		Attempt:   attempt,
		TraceID:   traceID,
		Method:    spec.Method,
		URL:       fullURL,
		Headers:   headers,
		Body:      bodyAny,
		Timestamp: time.Now().Format(time.RFC3339Nano),
	})
	cc.mu.Unlock()

	reqCtx, cancel := context.WithTimeout(ctx, cc.runner.opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, spec.Method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := cc.runner.httpClient.Do(req)
	if err != nil {
		cc.mu.Lock()
		cc.responses = append(cc.responses, responseLog{
			Seq:        seq,
			Attempt:    attempt,
			TraceID:    traceID,
			StatusCode: 0,
			DurationMS: time.Since(start).Milliseconds(),
			NetworkErr: err.Error(),
			ReceivedAt: time.Now().Format(time.RFC3339Nano),
		})
		cc.mu.Unlock()
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	cc.mu.Lock()
	cc.responses = append(cc.responses, responseLog{
		Seq:        seq,
		Attempt:    attempt,
		TraceID:    traceID,
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		BodyText:   string(body),
		DurationMS: time.Since(start).Milliseconds(),
		ReceivedAt: time.Now().Format(time.RFC3339Nano),
	})

	if spec.Stream {
		_, _ = fmt.Fprintf(&cc.streamRaw, "### trace=%s url=%s\n", traceID, fullURL)
		cc.streamRaw.Write(body)
		cc.streamRaw.WriteString("\n\n")
	}
	cc.mu.Unlock()

	return &responseResult{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
		TraceID:    traceID,
		URL:        fullURL,
	}, nil
}

func (cc *caseContext) flushArtifacts(cs caseResult) error {
	requestPath := filepath.Join(cc.dir, "request.json")
	headersPath := filepath.Join(cc.dir, "response.headers")
	bodyPath := filepath.Join(cc.dir, "response.body")
	streamPath := filepath.Join(cc.dir, "stream.raw")
	assertPath := filepath.Join(cc.dir, "assertions.json")
	metaPath := filepath.Join(cc.dir, "meta.json")

	if err := writeJSONFile(requestPath, cc.requests); err != nil {
		return err
	}
	respHeaders := make([]map[string]any, 0, len(cc.responses))
	respBodies := make([]map[string]any, 0, len(cc.responses))
	for _, r := range cc.responses {
		respHeaders = append(respHeaders, map[string]any{
			"seq":         r.Seq,
			"attempt":     r.Attempt,
			"trace_id":    r.TraceID,
			"status_code": r.StatusCode,
			"headers":     r.Headers,
		})
		respBodies = append(respBodies, map[string]any{
			"seq":           r.Seq,
			"attempt":       r.Attempt,
			"trace_id":      r.TraceID,
			"status_code":   r.StatusCode,
			"body_text":     r.BodyText,
			"network_error": r.NetworkErr,
			"duration_ms":   r.DurationMS,
		})
	}
	if err := writeJSONFile(headersPath, respHeaders); err != nil {
		return err
	}
	if err := writeJSONFile(bodyPath, respBodies); err != nil {
		return err
	}
	if err := os.WriteFile(streamPath, []byte(cc.streamRaw.String()), 0o644); err != nil {
		return err
	}
	if err := writeJSONFile(assertPath, cc.assertions); err != nil {
		return err
	}
	meta := map[string]any{
		"case_id":       cs.CaseID,
		"trace_id":      strings.Join(cs.TraceIDs, ","),
		"attempt":       len(cc.responses),
		"duration_ms":   cs.DurationMS,
		"status":        map[bool]string{true: "passed", false: "failed"}[cs.Passed],
		"status_codes":  cs.StatusCodes,
		"assertions":    cs.Assertions,
		"artifact_path": cs.ArtifactPath,
	}
	return writeJSONFile(metaPath, meta)
}
func (r *Runner) doSimpleJSON(ctx context.Context, method, path string, headers map[string]string, body any) (*responseResult, error) {
	cc := &caseContext{
		runner:      r,
		id:          "auth_prepare",
		traceIDsSet: map[string]struct{}{},
	}
	return cc.request(ctx, requestSpec{
		Method:    method,
		Path:      path,
		Headers:   headers,
		Body:      body,
		Retryable: true,
	})
}
