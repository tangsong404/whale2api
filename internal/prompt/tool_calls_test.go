package prompt

import "testing"

func TestStringifyToolCallArgumentsPreservesConcatenatedJSON(t *testing.T) {
	got := StringifyToolCallArguments(`{}{"query":"测试工具调用"}`)
	if got != `{}{"query":"测试工具调用"}` {
		t.Fatalf("expected raw concatenated JSON to be preserved, got %q", got)
	}
}

func TestFormatToolCallsForPromptColonMarkup(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"id": "call_1",
			"function": map[string]any{
				"name":      "search_web",
				"arguments": map[string]any{"query": "latest"},
			},
		},
	})
	if got == "" {
		t.Fatal("expected non-empty formatted tool calls")
	}
	want := "::tc::\n  ::invoke name=\"search_web\"::\n    ::param name=\"query\"::[[latest]]::/param::\n  ::/invoke::\n::/tc::"
	if got != want {
		t.Fatalf("unexpected formatted tool call markup: %q", got)
	}
}

func TestFormatToolCallsForPromptEscapesXMLEntities(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"name":      "search<&>",
			"arguments": `{"q":"a < b && c > d"}`,
		},
	})
	want := "::tc::\n  ::invoke name=\"search&lt;&amp;&gt;\"::\n    ::param name=\"q\"::[[a < b && c > d]]::/param::\n  ::/invoke::\n::/tc::"
	if got != want {
		t.Fatalf("unexpected escaped tool call XML: %q", got)
	}
}

func TestFormatToolCallsForPromptUsesRawForMultilineContent(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"name": "write_file",
			"arguments": map[string]any{
				"path":    "script.sh",
				"content": "#!/bin/bash\nprintf \"hello\"\n",
			},
		},
	})
	want := "::tc::\n  ::invoke name=\"write_file\"::\n    ::param name=\"content\"::[[#!/bin/bash\nprintf \"hello\"\n]]::/param::\n    ::param name=\"path\"::[[script.sh]]::/param::\n  ::/invoke::\n::/tc::"
	if got != want {
		t.Fatalf("unexpected multiline raw tool call markup: %q", got)
	}
}
