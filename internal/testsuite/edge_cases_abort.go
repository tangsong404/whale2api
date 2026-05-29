package testsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (cc *caseContext) abortStreamRequest(ctx context.Context, spec requestSpec) error {
	cc.seq++
	traceID := fmt.Sprintf("ts_%s_%s_%03d", cc.runner.runID, sanitizeID(cc.id), cc.seq)
	cc.traceIDsSet[traceID] = struct{}{}
	fullURL, err := withTraceQuery(cc.runner.baseURL+spec.Path, traceID)
	if err != nil {
		return err
	}
	headers := map[string]string{}
	for k, v := range spec.Headers {
		headers[k] = v
	}
	headers["X-Whale2-Test-Trace"] = traceID
	bodyBytes, _ := json.Marshal(spec.Body)
	headers["Content-Type"] = "application/json"
	cc.requests = append(cc.requests, requestLog{
		Seq:       cc.seq,
		Attempt:   1,
		TraceID:   traceID,
		Method:    spec.Method,
		URL:       fullURL,
		Headers:   headers,
		Body:      spec.Body,
		Timestamp: time.Now().Format(time.RFC3339Nano),
	})

	reqCtx, cancel := context.WithTimeout(ctx, cc.runner.opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, spec.Method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := cc.runner.httpClient.Do(req)
	if err != nil {
		cc.responses = append(cc.responses, responseLog{
			Seq:        cc.seq,
			Attempt:    1,
			TraceID:    traceID,
			StatusCode: 0,
			DurationMS: time.Since(start).Milliseconds(),
			NetworkErr: err.Error(),
			ReceivedAt: time.Now().Format(time.RFC3339Nano),
		})
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 512)
	_, _ = resp.Body.Read(buf)
	_ = resp.Body.Close()
	cc.responses = append(cc.responses, responseLog{
		Seq:        cc.seq,
		Attempt:    1,
		TraceID:    traceID,
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		BodyText:   "aborted_after_first_chunk",
		DurationMS: time.Since(start).Milliseconds(),
		ReceivedAt: time.Now().Format(time.RFC3339Nano),
	})
	return nil
}
