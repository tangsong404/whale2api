package completionruntime

import (
	"context"
	"net/http"
	"testing"

	"whale2api/internal/auth"
	"whale2api/internal/promptcompat"
)

func TestUserFacingTokenEstimateScalesInternalCount(t *testing.T) {
	if got := userFacingTokenEstimate(750_000); got != 256_000 {
		t.Fatalf("expected 256000 at gate, got %d", got)
	}
	if got := userFacingTokenEstimate(375_000); got != 128_000 {
		t.Fatalf("expected 128000 at half gate, got %d", got)
	}
}

func TestStartCompletionRejectsOverGateBeforeDeepSeek(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"no"}`)}}
	stdReq := promptcompat.StandardRequest{
		ResolvedModel:   "deepseek-v4-flash",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "",
		FinalPrompt:     "",
		RefFileTokens:   750_001,
	}

	_, outErr := StartCompletion(context.Background(), ds, &auth.RequestAuth{DeepSeekToken: "token"}, stdReq, Options{})
	if outErr == nil {
		t.Fatal("expected context_length_exceeded")
	}
	if outErr.Status != http.StatusBadRequest || outErr.Code != "context_length_exceeded" {
		t.Fatalf("unexpected error: %#v", outErr)
	}
	if outErr.Message == "" {
		t.Fatal("expected non-empty message")
	}
	if outErr.Param != "messages" {
		t.Fatalf("expected param messages, got %q", outErr.Param)
	}
	want := "This model's maximum context length is 256000 tokens. However, your messages resulted in 256000 tokens. Please reduce the length of the messages."
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

func TestStartCompletionAllowsAtGateBoundary(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"ok"}`)}}
	stdReq := promptcompat.StandardRequest{
		ResolvedModel:   "deepseek-v4-flash",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "",
		FinalPrompt:     "",
		RefFileTokens:   750_000,
	}

	_, outErr := StartCompletion(context.Background(), ds, &auth.RequestAuth{DeepSeekToken: "token"}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected error at gate boundary: %#v", outErr)
	}
	if ds.createSessions != 1 {
		t.Fatalf("expected CreateSession, got calls=%d", ds.createSessions)
	}
}
