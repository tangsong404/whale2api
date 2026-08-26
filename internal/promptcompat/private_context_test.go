package promptcompat

import (
	"strings"
	"testing"
)

func TestSplitMessagesForPrivateContextKeepsSystemLive(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "persona"},
		map[string]any{"role": "user", "content": "u1"},
		map[string]any{"role": "assistant", "content": "a1"},
		map[string]any{"role": "developer", "content": "dev rule"},
	}
	live, history := SplitMessagesForPrivateContext(messages)
	if len(live) != 2 {
		t.Fatalf("expected system+developer in live prefix, got %#v", live)
	}
	if len(history) != 2 {
		t.Fatalf("expected only dialogue history, got %#v", history)
	}
	transcript := BuildOpenAIPrivateContextTranscript(history)
	if strings.Contains(transcript, "persona") || strings.Contains(transcript, "dev rule") {
		t.Fatalf("transcript should omit system/developer, got %q", transcript)
	}
	if !strings.Contains(transcript, "User:\nu1") || !strings.Contains(transcript, "Assistant:\na1") {
		t.Fatalf("transcript missing dialogue, got %q", transcript)
	}
}

func TestBuildPrivateContextLiveMessagesKeepsSystemAndContinuation(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "persona"},
		map[string]any{"role": "user", "content": "old"},
	}
	live := BuildPrivateContextLiveMessages(messages)
	if len(live) != 2 {
		t.Fatalf("expected system + continuation user, got %#v", live)
	}
	sys := live[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "persona" {
		t.Fatalf("unexpected system message: %#v", sys)
	}
	user := live[1].(map[string]any)
	if user["role"] != "user" || user["content"] != PrivateContextLivePrompt {
		t.Fatalf("unexpected continuation message: %#v", user)
	}
}

func TestInjectToolPromptMergesIntoDeveloperRole(t *testing.T) {
	messages := []map[string]any{
		{"role": "developer", "content": "dev"},
		{"role": "user", "content": "hi"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": "search docs",
				"parameters":   map[string]any{"type": "object"},
			},
		},
	}
	out, names := injectToolPrompt(messages, tools, DefaultToolChoicePolicy())
	if len(names) != 1 || names[0] != "search" {
		t.Fatalf("unexpected names: %#v", names)
	}
	if len(out) != 2 {
		t.Fatalf("expected tool prompt merged into developer, got %#v", out)
	}
	content, _ := out[0]["content"].(string)
	if !strings.Contains(content, "dev") || !strings.Contains(content, "Tool-call format") {
		t.Fatalf("expected developer content plus tool instructions, got %q", content)
	}
	if strings.Contains(content, ToolActionNudgeMarker) {
		t.Fatalf("tool-action nudge must not be on developer/system, got %q", content)
	}
	userContent, _ := out[1]["content"].(string)
	if !strings.Contains(userContent, ToolActionNudgeMarker) {
		t.Fatalf("expected tool-action nudge on latest user message, got %q", userContent)
	}
}

func TestInjectToolPromptRequiredHasNoFakeRuleNumber(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "hi"},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "search",
				"description": "search docs",
				"parameters":   map[string]any{"type": "object"},
			},
		},
	}
	policy := ToolChoicePolicy{Mode: ToolChoiceRequired}
	out, _ := injectToolPrompt(messages, tools, policy)
	if len(out) != 2 {
		t.Fatalf("expected system tool prompt + user message, got %#v", out)
	}
	content, _ := out[0]["content"].(string)
	if !strings.Contains(content, "must call at least one tool from the allow-list") {
		t.Fatalf("missing required tool instruction, got %q", content)
	}
	userContent, _ := out[1]["content"].(string)
	if !strings.Contains(userContent, "hi") || !strings.Contains(userContent, ToolActionNudgeMarker) {
		t.Fatalf("expected user content with tool-action nudge, got %q", userContent)
	}
	if strings.Contains(content, ToolActionNudgeMarker) {
		t.Fatalf("tool-action nudge must not be on system, got %q", content)
	}
}
