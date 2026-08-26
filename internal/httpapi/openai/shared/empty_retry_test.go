package shared

import (
	"strings"
	"testing"
)

func TestRetrySuffixForTurn(t *testing.T) {
	if got := RetrySuffixForTurn("", true); got != EmptyOutputRetrySuffix {
		t.Fatalf("empty text should use empty suffix, got %q", got)
	}
	if got := RetrySuffixForTurn("narrative only", true); got != MissingToolCallRetrySuffix {
		t.Fatalf("text with tools should use missing-tool suffix, got %q", got)
	}
	if got := RetrySuffixForTurn("narrative only", false); got != EmptyOutputRetrySuffix {
		t.Fatalf("text without tools should fall back to empty suffix, got %q", got)
	}
}

func TestClonePayloadForAssistantRetry(t *testing.T) {
	payload := map[string]any{"prompt": "hello", "chat_session_id": "s1"}
	clone := ClonePayloadForAssistantRetry(payload, 42, MissingToolCallRetrySuffix)
	prompt, _ := clone["prompt"].(string)
	if !strings.Contains(prompt, MissingToolCallRetrySuffix) {
		t.Fatalf("expected missing-tool suffix in prompt, got %q", prompt)
	}
	if clone["parent_message_id"] != 42 {
		t.Fatalf("expected parent_message_id=42, got %#v", clone["parent_message_id"])
	}
	if payload["prompt"] != "hello" {
		t.Fatal("original payload must not be mutated")
	}
}

func TestRetryReasonLabel(t *testing.T) {
	if got := RetryReasonLabel("", true); got != "empty_output" {
		t.Fatalf("got %q", got)
	}
	if got := RetryReasonLabel("x", true); got != "missing_tool_calls" {
		t.Fatalf("got %q", got)
	}
}
