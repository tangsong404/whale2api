package promptcompat

import (
	"strings"
	"testing"
)

func TestAppendThinkingInjectionToSystemStringContent(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "persona"},
		map[string]any{"role": "user", "content": "older"},
		map[string]any{"role": "assistant", "content": "ok"},
		map[string]any{"role": "user", "content": "latest"},
	}

	out, changed := AppendThinkingInjectionToLatestUser(messages)
	if !changed {
		t.Fatal("expected thinking injection to be appended")
	}
	sys := out[0].(map[string]any)
	content, _ := sys["content"].(string)
	if !strings.Contains(content, "persona\n\n"+ThinkingInjectionMarker) {
		t.Fatalf("expected injection after system text, got %q", content)
	}
	latest := out[3].(map[string]any)
	if latest["content"] != "latest" {
		t.Fatalf("expected latest user message unchanged, got %#v", latest["content"])
	}
}

func TestAppendThinkingInjectionCreatesSystemWhenMissing(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "latest"},
	}

	out, changed := AppendThinkingInjectionToLatestUser(messages)
	if !changed {
		t.Fatal("expected thinking injection to be appended")
	}
	if len(out) != 2 {
		t.Fatalf("expected prepended system message, got %#v", out)
	}
	sys := out[0].(map[string]any)
	if sys["role"] != "system" {
		t.Fatalf("expected system role, got %#v", sys["role"])
	}
	content, _ := sys["content"].(string)
	if !strings.Contains(content, ThinkingInjectionMarker) {
		t.Fatalf("expected thinking marker in system, got %q", content)
	}
	if out[1].(map[string]any)["content"] != "latest" {
		t.Fatalf("expected user content unchanged, got %#v", out[1])
	}
}

func TestAppendThinkingInjectionToSystemArrayContent(t *testing.T) {
	messages := []any{
		map[string]any{
			"role": "system",
			"content": []any{
				map[string]any{"type": "text", "text": "persona"},
			},
		},
		map[string]any{"role": "user", "content": "latest"},
	}

	out, changed := AppendThinkingInjectionToLatestUser(messages)
	if !changed {
		t.Fatal("expected thinking injection to be appended")
	}
	content, _ := out[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected appended text block, got %#v", content)
	}
	block, _ := content[1].(map[string]any)
	if block["type"] != "text" || !strings.Contains(block["text"].(string), ThinkingInjectionMarker) {
		t.Fatalf("unexpected appended block: %#v", block)
	}
}

func TestAppendThinkingInjectionCustomPrompt(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "latest"},
	}

	out, changed := AppendThinkingInjectionPromptToLatestUser(messages, "custom thinking format")
	if !changed {
		t.Fatal("expected custom thinking injection to be appended")
	}
	content, _ := out[0].(map[string]any)["content"].(string)
	if content != "custom thinking format" {
		t.Fatalf("expected custom injection as system, got %q", content)
	}
}

func TestAppendThinkingInjectionSkipsDuplicate(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": DefaultThinkingInjectionPrompt},
		map[string]any{"role": "user", "content": "latest"},
	}

	out, changed := AppendThinkingInjectionToLatestUser(messages)
	if changed {
		t.Fatal("expected duplicate injection to be skipped")
	}
	if len(out) != 2 {
		t.Fatalf("unexpected messages: %#v", out)
	}
}
