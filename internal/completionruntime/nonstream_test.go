package completionruntime

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"whale2api/internal/auth"
	dsclient "whale2api/internal/deepseek/client"
	"whale2api/internal/promptcompat"
)

type fakeDeepSeekCaller struct {
	responses             []*http.Response
	payloads              []map[string]any
	uploads               []dsclient.UploadFileRequest
	uploadAccounts        []string
	switchAccountOnCreate string
	createSessions        int
}

func (f *fakeDeepSeekCaller) CreateSession(_ context.Context, a *auth.RequestAuth, _ int) (string, error) {
	f.createSessions++
	if f.switchAccountOnCreate != "" {
		a.AccountID = f.switchAccountOnCreate
	}
	return "session-1", nil
}

func (f *fakeDeepSeekCaller) GetPow(context.Context, *auth.RequestAuth, int) (string, error) {
	return "pow", nil
}

func (f *fakeDeepSeekCaller) UploadFile(_ context.Context, a *auth.RequestAuth, req dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	f.uploads = append(f.uploads, req)
	f.uploadAccounts = append(f.uploadAccounts, a.AccountID)
	return &dsclient.UploadFileResult{ID: "file-runtime-" + a.AccountID}, nil
}

func (f *fakeDeepSeekCaller) CallCompletion(_ context.Context, _ *auth.RequestAuth, payload map[string]any, _ string, _ int) (*http.Response, error) {
	f.payloads = append(f.payloads, payload)
	if len(f.responses) == 0 {
		return sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"fallback"}`), nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func TestExecuteNonStreamWithRetryBuildsCanonicalTurn(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(
		http.StatusOK,
		`data: {"response_message_id":42,"p":"response/content","v":"<tool_calls><invoke name=\"Write\"><parameter name=\"content\">{\"x\":1}</parameter></invoke></tool_calls>"}`,
	)}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
		ToolNames:       []string{"Write"},
		ToolsRaw: []any{map[string]any{
			"name": "Write",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
				},
			},
		}},
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if result.SessionID != "session-1" {
		t.Fatalf("session mismatch: %q", result.SessionID)
	}
	if got := result.Turn.ResponseMessageID; got != 42 {
		t.Fatalf("response message id mismatch: %d", got)
	}
	if len(result.Turn.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(result.Turn.ToolCalls))
	}
	if _, ok := result.Turn.ToolCalls[0].Input["content"].(string); !ok {
		t.Fatalf("expected schema-normalized string argument, got %#v", result.Turn.ToolCalls[0].Input["content"])
	}
	if result.Turn.Usage.InputTokens == 0 || result.Turn.Usage.TotalTokens == 0 {
		t.Fatalf("expected usage to be populated, got %#v", result.Turn.Usage)
	}
}

func TestExecuteNonStreamWithRetryUsesSameAccountSyntheticEmptyRetry(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		sseHTTPResponse(http.StatusOK, `data: {"response_message_id":77,"p":"response/status","v":"FINISHED"}`),
		sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"visible"}`),
	}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{RetryEnabled: true})
	if outErr != nil {
		t.Fatalf("expected synthetic retry success, got outErr=%#v", outErr)
	}
	if len(ds.payloads) != 2 {
		t.Fatalf("expected same-account synthetic retry, got %d completion calls", len(ds.payloads))
	}
	if result.Turn.Text != "visible" {
		t.Fatalf("expected retry visible text, got %q", result.Turn.Text)
	}
}

func TestExecuteNonStreamWithRetryConvertsReferenceMarkers(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(
		http.StatusOK,
		`data: {"p":"response/content","v":"答案[reference:0]。","citation":{"cite_index":0,"url":"https://example.com/ref"}}`,
	)}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash-search",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
		Search:          true,
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	want := "答案[0](https://example.com/ref)。"
	if result.Turn.Text != want {
		t.Fatalf("text mismatch: got %q want %q", result.Turn.Text, want)
	}
}

func TestStartCompletionDoesNotUploadHistoryContextFile(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"ok"}`)}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test_adapter",
		RequestedModel:  "deepseek-v4-flash",
		ResolvedModel:   "deepseek-v4-flash",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "first user turn",
		FinalPrompt:     "first user turn",
		Messages: []any{
			map[string]any{"role": "user", "content": "first user turn"},
		},
	}

	start, outErr := StartCompletion(context.Background(), ds, &auth.RequestAuth{DeepSeekToken: "token"}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if len(ds.uploads) != 0 {
		t.Fatalf("expected no context file upload, got %d", len(ds.uploads))
	}
	if len(ds.payloads) != 1 {
		t.Fatalf("expected one completion payload, got %d", len(ds.payloads))
	}
	prompt, _ := ds.payloads[0]["prompt"].(string)
	if prompt != "first user turn" {
		t.Fatalf("expected prompt unchanged, got %q", prompt)
	}
	if start.Request.PromptTokenText != "first user turn" {
		t.Fatalf("expected PromptTokenText unchanged, got %q", start.Request.PromptTokenText)
	}
}

func TestStartCompletionReuploadsPrivateContextAfterAccountSwitch(t *testing.T) {
	ds := &fakeDeepSeekCaller{
		switchAccountOnCreate: "acct-b",
		responses: []*http.Response{
			sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"ok"}`),
		},
	}
	stdReq := promptcompat.StandardRequest{
		Surface:                 "openai_chat",
		RequestedModel:          "deepseek-v4-flash",
		ResolvedModel:           "deepseek-v4-flash",
		ResponseModel:           "deepseek-v4-flash",
		PromptTokenText:         "history\nlatest",
		FinalPrompt:             "latest",
		RefFileIDs:              []string{"file-old", "file-user"},
		PrivateContextText:      "用户:\nold turn\n",
		PrivateContextAccountID: "acct-a",
		PrivateContextRefFileID: "file-old",
	}

	start, outErr := StartCompletion(context.Background(), ds, &auth.RequestAuth{AccountID: "acct-a", DeepSeekToken: "token"}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if len(ds.uploads) != 1 {
		t.Fatalf("expected private context reupload after account switch, got %d", len(ds.uploads))
	}
	if len(ds.uploadAccounts) != 1 || ds.uploadAccounts[0] != "acct-b" {
		t.Fatalf("expected reupload under switched account, got %#v", ds.uploadAccounts)
	}
	refs := start.Payload["ref_file_ids"].([]any)
	if len(refs) != 2 || refs[0] != "file-runtime-acct-b" || refs[1] != "file-user" {
		t.Fatalf("expected old private file id replaced, got %#v", refs)
	}
	if start.Request.PrivateContextAccountID != "acct-b" || start.Request.PrivateContextRefFileID != "file-runtime-acct-b" {
		t.Fatalf("expected returned request metadata updated, got account=%q file=%q", start.Request.PrivateContextAccountID, start.Request.PrivateContextRefFileID)
	}
}

func sseHTTPResponse(status int, lines ...string) *http.Response {
	body := strings.Join(lines, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
