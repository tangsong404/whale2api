package chat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"whale2api/internal/promptcompat"
)

func TestChatStreamKeepAliveEmitsEmptyChoiceDataFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		true,
		"chatcmpl-test",
		time.Now().Unix(),
		"deepseek-v4-flash",
		"prompt",
		false,
		false,
		true,
		nil,
		nil,
		promptcompat.DefaultToolChoicePolicy(),
		false,
		false,
	)

	runtime.sendKeepAlive()

	body := rec.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") {
		t.Fatalf("expected keep-alive comment, got %q", body)
	}
	frames, done := parseSSEDataFrames(t, body)
	if done {
		t.Fatalf("keep-alive must not emit [DONE], body=%q", body)
	}
	if len(frames) != 1 {
		t.Fatalf("expected one data frame, got %d body=%q", len(frames), body)
	}
	if got := asString(frames[0]["id"]); got != "chatcmpl-test" {
		t.Fatalf("expected completion id to be preserved, got %q", got)
	}
	if got := asString(frames[0]["object"]); got != "chat.completion.chunk" {
		t.Fatalf("expected chat chunk object, got %q", got)
	}
	choices, _ := frames[0]["choices"].([]any)
	if len(choices) != 0 {
		t.Fatalf("expected empty choices heartbeat, got %#v", choices)
	}
}

func TestChatStreamFinalizeEnforcesRequiredToolChoice(t *testing.T) {
	rec := httptest.NewRecorder()
	runtime := newChatStreamRuntime(
		rec,
		http.NewResponseController(rec),
		true,
		"chatcmpl-test",
		time.Now().Unix(),
		"deepseek-v4-flash",
		"prompt",
		false,
		false,
		true,
		[]string{"Write"},
		nil,
		promptcompat.ToolChoicePolicy{Mode: promptcompat.ToolChoiceRequired},
		true,
		false,
	)

	if !runtime.finalize("stop", false) {
		t.Fatalf("expected terminal error to be written")
	}
	if runtime.finalErrorCode != "tool_choice_violation" {
		t.Fatalf("expected tool_choice_violation, got %q body=%s", runtime.finalErrorCode, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tool_choice requires") {
		t.Fatalf("expected tool choice error in stream body, got %s", rec.Body.String())
	}
}
