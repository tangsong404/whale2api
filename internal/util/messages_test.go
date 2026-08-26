package util

import (
	"strings"
	"testing"

	"whale2api/internal/prompt"
)

func TestMessagesPrepareBasic(t *testing.T) {
	messages := []map[string]any{{"role": "user", "content": "Hello"}}
	got := MessagesPrepare(messages)
	if got == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !strings.HasPrefix(got, prompt.SystemOpenMarker) {
		t.Fatalf("expected output integrity guard at the start, got %q", got)
	}
	if !strings.Contains(got, "Hello") || !strings.HasSuffix(got, prompt.AssistantOpenMarker) {
		t.Fatalf("unexpected prompt: %q", got)
	}
}

func TestMessagesPrepareRoles(t *testing.T) {
	messages := []map[string]any{
		{"role": "system", "content": "You are helper"},
		{"role": "user", "content": "Hi"},
		{"role": "assistant", "content": "Hello"},
		{"role": "tool", "content": "Search results"},
		{"role": "user", "content": "How are you"},
	}
	got := MessagesPrepare(messages)
	if !contains(got, "Output integrity note") {
		t.Fatalf("expected output integrity guard in %q", got)
	}
	if !contains(got, "You are helper") || !contains(got, prompt.UserOpenMarker+"Hi") {
		t.Fatalf("expected system/user content in %q", got)
	}
	if !contains(got, prompt.UserOpenMarker+"Hi"+prompt.AssistantOpenMarker+"Hello"+prompt.AssistantCloseMarker) {
		t.Fatalf("expected user/assistant separation in %q", got)
	}
	if !contains(got, prompt.AssistantOpenMarker+"Hello"+prompt.AssistantCloseMarker+prompt.ToolOpenMarker+"Search results"+prompt.ToolCloseMarker) {
		t.Fatalf("expected assistant/tool separation in %q", got)
	}
	if !contains(got, prompt.ToolOpenMarker+"Search results"+prompt.ToolCloseMarker+prompt.UserOpenMarker+"How are you") {
		t.Fatalf("expected tool/user separation in %q", got)
	}
	if !contains(got, prompt.AssistantOpenMarker) {
		t.Fatalf("expected assistant marker in %q", got)
	}
	if !contains(got, prompt.SystemOpenMarker) {
		t.Fatalf("expected system marker in %q", got)
	}
	if !contains(got, prompt.UserOpenMarker) {
		t.Fatalf("expected user marker in %q", got)
	}
	if !contains(got, prompt.ToolOpenMarker) {
		t.Fatalf("expected tool marker in %q", got)
	}
}

func TestMessagesPrepareObjectContent(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": map[string]any{"temp": 18, "ok": true}},
	}
	got := MessagesPrepare(messages)
	if !contains(got, `"temp":18`) || !contains(got, `"ok":true`) {
		t.Fatalf("expected serialized object content, got %q", got)
	}
}

func TestMessagesPrepareArrayTextVariants(t *testing.T) {
	messages := []map[string]any{
		{
			"role": "user",
			"content": []any{
				map[string]any{"type": "output_text", "text": "line1"},
				map[string]any{"type": "input_text", "text": "line2"},
				map[string]any{"type": "image_url", "image_url": "https://example.com/a.png"},
			},
		},
	}
	got := MessagesPrepare(messages)
	if !contains(got, "line1\nline2") {
		t.Fatalf("unexpected content from text variants: %q", got)
	}
	if !strings.Contains(got, "Output integrity note") {
		t.Fatalf("expected output integrity guard in %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
