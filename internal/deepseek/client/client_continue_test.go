package client

import (
	"bytes"
	"context"
	dsprotocol "whale2api/internal/deepseek/protocol"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"whale2api/internal/auth"
)

type failingDoer struct {
	err error
}

func (d failingDoer) Do(_ *http.Request) (*http.Response, error) {
	return nil, d.err
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCallContinuePropagatesPowHeaderToFallbackRequest(t *testing.T) {
	var seenPow string
	var seenURL string

	client := &Client{
		stream: failingDoer{err: errors.New("stream transport failed")},
		fallbackS: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				seenPow = req.Header.Get("x-ds-pow-response")
				seenURL = req.URL.String()
				body := io.NopCloser(strings.NewReader("data: {\"p\":\"response/content\",\"v\":\"continued\"}\n" + "data: [DONE]\n"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       body,
					Request:    req,
				}, nil
			}),
		},
	}

	resp, err := client.callContinue(context.Background(), &auth.RequestAuth{
		DeepSeekToken: "token",
		AccountID:     "acct",
	}, "session-123", 99, "pow-response-abc")
	if err != nil {
		t.Fatalf("callContinue returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if seenPow != "pow-response-abc" {
		t.Fatalf("continue request pow header=%q want=%q", seenPow, "pow-response-abc")
	}
	if seenURL != dsprotocol.DeepSeekContinueURL {
		t.Fatalf("continue request url=%q want=%q", seenURL, dsprotocol.DeepSeekContinueURL)
	}
}

func TestCallCompletionAutoContinueThreadsPowHeader(t *testing.T) {
	var seenPow string
	var seenContinueURL string

	initialBody := strings.Join([]string{
		`data: {"response_message_id":321,"v":{"response":{"message_id":321,"status":"WIP","auto_continue":true}}}`,
		`data: [DONE]`,
	}, "\n") + "\n"

	client := &Client{
		stream: failingOrCompletionDoer{
			completionResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(initialBody)),
			},
		},
		fallbackS: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				seenPow = req.Header.Get("x-ds-pow-response")
				seenContinueURL = req.URL.String()
				body := io.NopCloser(strings.NewReader("data: {\"response_message_id\":322,\"v\":{\"response\":{\"message_id\":322,\"status\":\"FINISHED\"}}}\n" + "data: [DONE]\n"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       body,
					Request:    req,
				}, nil
			}),
		},
	}

	resp, err := client.CallCompletion(context.Background(), &auth.RequestAuth{
		DeepSeekToken: "token",
		AccountID:     "acct",
	}, map[string]any{
		"chat_session_id": "session-123",
	}, "pow-response-xyz", 1)
	if err != nil {
		t.Fatalf("CallCompletion returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read auto-continued body failed: %v", err)
	}
	if seenPow != "pow-response-xyz" {
		t.Fatalf("threaded continue pow header=%q want=%q", seenPow, "pow-response-xyz")
	}
	if seenContinueURL != dsprotocol.DeepSeekContinueURL {
		t.Fatalf("continue url=%q want=%q", seenContinueURL, dsprotocol.DeepSeekContinueURL)
	}
	if !bytes.Contains(out, []byte(`"status":"WIP"`)) {
		t.Fatalf("expected initial stream content in body, got=%s", string(out))
	}
	if !bytes.Contains(out, []byte(`data: [DONE]`)) {
		t.Fatalf("expected final DONE sentinel in body, got=%s", string(out))
	}
}

func TestAutoContinueDoesNotTriggerOnPlainWIPWithoutExplicitContinuationSignal(t *testing.T) {
	initialBody := strings.Join([]string{
		`data: {"response_message_id":321,"v":{"response":{"message_id":321,"status":"WIP","auto_continue":false}}}`,
		`data: [DONE]`,
	}, "\n") + "\n"

	var continueCalls atomic.Int32
	body := newAutoContinueBody(context.Background(), io.NopCloser(strings.NewReader(initialBody)), "session-123", 8, func(context.Context, string, int) (*http.Response, error) {
		continueCalls.Add(1)
		return nil, errors.New("continue should not have been called")
	})
	defer func() { _ = body.Close() }()

	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if continueCalls.Load() != 0 {
		t.Fatalf("expected no continue calls, got %d", continueCalls.Load())
	}
	if !bytes.Contains(out, []byte(`"status":"WIP"`)) || !bytes.Contains(out, []byte(`data: [DONE]`)) {
		t.Fatalf("expected original body to pass through unchanged, got=%s", string(out))
	}
}

func TestAutoContinuePassesThroughLongSingleSSELine(t *testing.T) {
	payload := strings.Repeat("x", 2*1024*1024+4096)
	initialBody := `data: {"p":"response/content","v":"` + payload + `"}` + "\n" +
		`data: [DONE]` + "\n"

	body := newAutoContinueBody(context.Background(), io.NopCloser(strings.NewReader(initialBody)), "session-123", 8, func(context.Context, string, int) (*http.Response, error) {
		return nil, errors.New("continue should not have been called")
	})
	defer func() { _ = body.Close() }()

	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if !bytes.Contains(out, []byte(payload)) {
		t.Fatalf("expected long SSE payload to pass through, got len=%d want payload len=%d", len(out), len(payload))
	}
	if !bytes.Contains(out, []byte(`data: [DONE]`)) {
		t.Fatalf("expected final DONE sentinel in body, got len=%d", len(out))
	}
}

func TestAutoContinueTriggersOnDirectQuasiStatusIncomplete(t *testing.T) {
	initialBody := strings.Join([]string{
		`data: {"response_message_id":321,"p":"response/content","v":"<tool_calls><invoke name=\"write_file\"><parameter name=\"content\"><![CDATA[part-one"}`,
		`data: {"p":"response/quasi_status","v":"INCOMPLETE"}`,
		`data: [DONE]`,
	}, "\n") + "\n"

	var continueCalls atomic.Int32
	body := newAutoContinueBody(context.Background(), io.NopCloser(strings.NewReader(initialBody)), "session-123", 8, func(context.Context, string, int) (*http.Response, error) {
		continueCalls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`data: {"response_message_id":322,"p":"response/content","v":"-part-two]]></parameter></invoke></tool_calls>"}` + "\n" +
					`data: {"p":"response/status","v":"FINISHED"}` + "\n" +
					`data: [DONE]` + "\n",
			)),
		}, nil
	})
	defer func() { _ = body.Close() }()

	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if continueCalls.Load() != 1 {
		t.Fatalf("expected exactly one continue call, got %d", continueCalls.Load())
	}
	if !bytes.Contains(out, []byte("part-one")) || !bytes.Contains(out, []byte("-part-two")) {
		t.Fatalf("expected continued tool content in body, got=%s", string(out))
	}
}

func TestAutoContinueTriggersOnResponseBatchQuasiStatusIncomplete(t *testing.T) {
	initialBody := strings.Join([]string{
		`data: {"response_message_id":321,"v":{"response":{"message_id":321,"status":"WIP","auto_continue":false}}}`,
		`data: {"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":2413},{"p":"quasi_status","v":"INCOMPLETE"}]}`,
		`data: [DONE]`,
	}, "\n") + "\n"

	var continueCalls atomic.Int32
	body := newAutoContinueBody(context.Background(), io.NopCloser(strings.NewReader(initialBody)), "session-123", 8, func(context.Context, string, int) (*http.Response, error) {
		continueCalls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`data: {"response_message_id":322,"p":"response/status","v":"FINISHED"}` + "\n" +
					`data: [DONE]` + "\n",
			)),
		}, nil
	})
	defer func() { _ = body.Close() }()

	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if continueCalls.Load() != 1 {
		t.Fatalf("expected exactly one continue call, got %d", continueCalls.Load())
	}
	if !bytes.Contains(out, []byte(`"quasi_status","v":"INCOMPLETE"`)) || !bytes.Contains(out, []byte(`"v":"FINISHED"`)) {
		t.Fatalf("expected continued output to include initial and final rounds, got=%s", string(out))
	}
}

func TestAutoContinueDoesNotTriggerWhenResponseBatchQuasiStatusFinished(t *testing.T) {
	initialBody := strings.Join([]string{
		`data: {"response_message_id":321,"v":{"response":{"message_id":321,"status":"WIP","auto_continue":false}}}`,
		`data: {"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":2413},{"p":"quasi_status","v":"FINISHED"}]}`,
		`data: [DONE]`,
	}, "\n") + "\n"

	var continueCalls atomic.Int32
	body := newAutoContinueBody(context.Background(), io.NopCloser(strings.NewReader(initialBody)), "session-123", 8, func(context.Context, string, int) (*http.Response, error) {
		continueCalls.Add(1)
		return nil, errors.New("continue should not have been called")
	})
	defer func() { _ = body.Close() }()

	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if continueCalls.Load() != 0 {
		t.Fatalf("expected no continue calls, got %d", continueCalls.Load())
	}
	if !bytes.Contains(out, []byte(`"quasi_status","v":"FINISHED"`)) || !bytes.Contains(out, []byte(`data: [DONE]`)) {
		t.Fatalf("expected original finished body to pass through unchanged, got=%s", string(out))
	}
}

type failingOrCompletionDoer struct {
	completionResp *http.Response
}

func (d failingOrCompletionDoer) Do(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "/chat/completion") {
		return d.completionResp, nil
	}
	return nil, errors.New("forced stream failure")
}

func TestAutoContinuePreservesIncompleteStateWhenNextChunkOmitsStatus(t *testing.T) {
	initialBody := strings.Join([]string{
		`data: {"response_message_id":321,"v":{"response":{"message_id":321,"status":"INCOMPLETE"}}}`,
		`data: {"p":"response/content","v":{"text":"continued"}}`,
		`data: [DONE]`,
	}, "\n") + "\n"

	var continueCalls atomic.Int32
	body := newAutoContinueBody(context.Background(), io.NopCloser(strings.NewReader(initialBody)), "session-123", 8, func(context.Context, string, int) (*http.Response, error) {
		continueCalls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`data: {"response_message_id":322,"p":"response/status","v":"FINISHED"}` + "\n" +
					`data: [DONE]` + "\n",
			)),
		}, nil
	})
	defer func() { _ = body.Close() }()

	_, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if continueCalls.Load() != 1 {
		t.Fatalf("expected exactly one continue call, got %d", continueCalls.Load())
	}
}
