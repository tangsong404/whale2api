package promptcompat

import "testing"

func TestNormalizeResponsesInputItemPreservesAssistantReasoningContent(t *testing.T) {
	item := map[string]any{
		"role":              "assistant",
		"reasoning_content": "hidden reasoning",
		"tool_calls": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":      "search",
					"arguments": `{"q":"docs"}`,
				},
			},
		},
	}

	got := normalizeResponsesInputItem(item)
	if got == nil {
		t.Fatal("expected assistant item to be preserved")
	}
	if got["role"] != "assistant" {
		t.Fatalf("unexpected role: %#v", got["role"])
	}
	if got["reasoning_content"] != "hidden reasoning" {
		t.Fatalf("expected reasoning_content preserved, got %#v", got["reasoning_content"])
	}
}

func TestNormalizeResponsesInputItemAssistantMessageWithReasoningBlocks(t *testing.T) {
	item := map[string]any{
		"type": "message",
		"role": "assistant",
		"content": []any{
			map[string]any{"type": "reasoning", "text": "internal chain"},
			map[string]any{"type": "output_text", "text": "visible answer"},
		},
	}

	got := normalizeResponsesInputItem(item)
	if got == nil {
		t.Fatal("expected assistant message item to be preserved")
	}
	content, _ := got["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected content blocks preserved, got %#v", got["content"])
	}
}
