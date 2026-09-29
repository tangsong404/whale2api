package completionruntime

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"whale2api/internal/auth"
	"whale2api/internal/promptcompat"
)

func TestUserFacingTokenEstimateScalesInternalCount(t *testing.T) {
	if got := userFacingTokenEstimate(2_850_000); got != 896_000 {
		t.Fatalf("expected 896000 at gate, got %d", got)
	}
	if got := userFacingTokenEstimate(1_425_000); got != 448_000 {
		t.Fatalf("expected 448000 at half gate, got %d", got)
	}
}

func TestStartCompletionRejectsOverGateBeforeDeepSeek(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"no"}`)}}
	stdReq := promptcompat.StandardRequest{
		ResolvedModel:   "deepseek-flash",
		ResponseModel:   "deepseek-flash",
		PromptTokenText: "",
		FinalPrompt:     "",
		RefFileTokens:   2_850_001,
		PassThrough:     map[string]any{"max_tokens": float64(100)},
	}

	_, outErr := StartCompletion(context.Background(), ds, &auth.RequestAuth{DeepSeekToken: "token"}, stdReq, Options{})
	if outErr == nil {
		t.Fatal("expected context length error")
	}
	if outErr.Status != http.StatusBadRequest || outErr.Code != "invalid_request_error" {
		t.Fatalf("unexpected error: %#v", outErr)
	}
	if outErr.Message == "" {
		t.Fatal("expected non-empty message")
	}
	if outErr.Param != "" {
		t.Fatalf("expected empty param, got %q", outErr.Param)
	}
	want := "This model's maximum context length is 896000 tokens. However, you requested 896100 tokens (896000 in the messages, 100 in the completion). Please reduce the length of the messages or completion."
	if outErr.Message != want {
		t.Fatalf("unexpected message: %q", outErr.Message)
	}
	if ds.createSessions != 0 {
		t.Fatalf("expected CreateSession skipped, got calls=%d", ds.createSessions)
	}
	if len(ds.payloads) != 0 {
		t.Fatalf("expected CallCompletion skipped, got payloads=%d", len(ds.payloads))
	}
}

func TestContextGateErrorChargesPrivateContextUploadTokens(t *testing.T) {
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "deepseek-flash",
		RefFileTokens: 2_800_000,
		Messages: []any{map[string]any{
			"role":    "user",
			"content": strings.Repeat("a", 300_000),
		}},
	}

	// Base estimate fits, but the prospective private-context upload
	// (transcript runes / 3) pushes it over the gate.
	if err := ContextGateError(stdReq); err == nil {
		t.Fatal("expected gate error with private-context upload tokens")
	}

	stdReq.Messages = nil
	if err := ContextGateError(stdReq); err != nil {
		t.Fatalf("unexpected gate error without upload: %#v", err)
	}
}

func TestStartCompletionAllowsAtGateBoundary(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"ok"}`)}}
	stdReq := promptcompat.StandardRequest{
		ResolvedModel:   "deepseek-flash",
		ResponseModel:   "deepseek-flash",
		PromptTokenText: "",
		FinalPrompt:     "",
		RefFileTokens:   2_850_000,
	}

	_, outErr := StartCompletion(context.Background(), ds, &auth.RequestAuth{DeepSeekToken: "token"}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected error at gate boundary: %#v", outErr)
	}
	if ds.createSessions != 1 {
		t.Fatalf("expected CreateSession, got calls=%d", ds.createSessions)
	}
}
