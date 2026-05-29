package prompt

import "testing"

func TestStringifyToolCallArgumentsPreservesConcatenatedJSON(t *testing.T) {
	got := StringifyToolCallArguments(`{}{"query":"测试工具调用"}`)
	if got != `{}{"query":"测试工具调用"}` {
		t.Fatalf("expected raw concatenated JSON to be preserved, got %q", got)
	}
}

func TestFormatToolCallsForPromptZJML(t *testing.T) {
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
	want := "<|ZJML|工具调用>\n  <|ZJML|调用项 name=\"search_web\">\n    <|ZJML|形参 name=\"query\"><![CDATA[latest]]></|ZJML|形参>\n  </|ZJML|调用项>\n</|ZJML|工具调用>"
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
	want := "<|ZJML|工具调用>\n  <|ZJML|调用项 name=\"search&lt;&amp;&gt;\">\n    <|ZJML|形参 name=\"q\"><![CDATA[a < b && c > d]]></|ZJML|形参>\n  </|ZJML|调用项>\n</|ZJML|工具调用>"
	if got != want {
		t.Fatalf("unexpected escaped tool call XML: %q", got)
	}
}

func TestFormatToolCallsForPromptUsesCDATAForMultilineContent(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"name": "write_file",
			"arguments": map[string]any{
				"path":    "script.sh",
				"content": "#!/bin/bash\nprintf \"hello\"\n",
			},
		},
	})
	want := "<|ZJML|工具调用>\n  <|ZJML|调用项 name=\"write_file\">\n    <|ZJML|形参 name=\"content\"><![CDATA[#!/bin/bash\nprintf \"hello\"\n]]></|ZJML|形参>\n    <|ZJML|形参 name=\"path\"><![CDATA[script.sh]]></|ZJML|形参>\n  </|ZJML|调用项>\n</|ZJML|工具调用>"
	if got != want {
		t.Fatalf("unexpected multiline cdata tool call XML: %q", got)
	}
}
