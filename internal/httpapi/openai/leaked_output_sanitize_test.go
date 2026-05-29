package openai

import "testing"

func TestSanitizeLeakedOutputRemovesEmptyJSONFence(t *testing.T) {
	raw := "before\n```json\n```\nafter"
	got := sanitizeLeakedOutput(raw)
	if got != "before\n\nafter" {
		t.Fatalf("unexpected sanitized empty json fence: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesLeakedWireToolCallAndResult(t *testing.T) {
	raw := "开始\n[{\"function\":{\"arguments\":\"{\\\"command\\\":\\\"java -version\\\"}\",\"name\":\"exec\"},\"id\":\"callb9a321\",\"type\":\"function\"}]< | Tool | >{\"content\":\"openjdk version 21\",\"tool_call_id\":\"callb9a321\"}\n结束"
	got := sanitizeLeakedOutput(raw)
	if got != "开始\n\n结束" {
		t.Fatalf("unexpected sanitize result for leaked wire format: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesStandaloneMetaMarkers(t *testing.T) {
	raw := "A<| end_of_sentence |><| Assistant |>B<| end_of_thinking |>C<｜end▁of▁thinking｜>D<｜end▁of▁sentence｜>E<| end_of_toolresults |>F<｜end▁of▁instructions｜>G"
	got := sanitizeLeakedOutput(raw)
	if got != "ABCDEFG" {
		t.Fatalf("unexpected sanitize result for meta markers: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesThinkAndBosMarkers(t *testing.T) {
	raw := "A<think>B</think>C<｜begin▁of▁sentence｜>D<| begin_of_sentence |>E<｜begin_of_sentence｜>F"
	got := sanitizeLeakedOutput(raw)
	if got != "ABCDEF" {
		t.Fatalf("unexpected sanitize result for think/BOS markers: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesDanglingThinkBlock(t *testing.T) {
	raw := "Answer prefix<think>internal reasoning that never closes"
	got := sanitizeLeakedOutput(raw)
	if got != "Answer prefix" {
		t.Fatalf("unexpected sanitize result for dangling think block: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesCompleteDSMLToolCallWrapper(t *testing.T) {
	raw := "前置文本\n<｜DSML｜tool_calls>\n<｜DSML｜invoke name=\"Bash\">\n<｜DSML｜parameter name=\"command\"></｜DSML｜parameter>\n</｜DSML｜invoke>\n</｜DSML｜tool_calls>\n后置文本"
	got := sanitizeLeakedOutput(raw)
	if got != "前置文本\n\n后置文本" {
		t.Fatalf("unexpected sanitize result for leaked dsml wrapper: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesAgentXMLLeaks(t *testing.T) {
	raw := "Done.<attempt_completion><result>Some final answer</result></attempt_completion>"
	got := sanitizeLeakedOutput(raw)
	if got != "Done.Some final answer" {
		t.Fatalf("unexpected sanitize result for agent XML leak: %q", got)
	}
}

func TestSanitizeLeakedOutputPreservesStandaloneResultTags(t *testing.T) {
	raw := "Example XML: <result>value</result>"
	got := sanitizeLeakedOutput(raw)
	if got != raw {
		t.Fatalf("unexpected sanitize result for standalone result tag: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesDanglingAgentXMLOpeningTags(t *testing.T) {
	raw := "Done.<attempt_completion><result>Some final answer"
	got := sanitizeLeakedOutput(raw)
	if got != "Done.Some final answer" {
		t.Fatalf("unexpected sanitize result for dangling opening tags: %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesDanglingAgentXMLClosingTags(t *testing.T) {
	raw := "Done.Some final answer</result></attempt_completion>"
	got := sanitizeLeakedOutput(raw)
	if got != "Done.Some final answer" {
		t.Fatalf("unexpected sanitize result for dangling closing tags: %q", got)
	}
}

func TestSanitizeLeakedOutputPreservesUnrelatedResultTagsWhenWrapperLeaks(t *testing.T) {
	raw := "Done.<attempt_completion><result>Some final answer\nExample XML: <result>value</result>"
	got := sanitizeLeakedOutput(raw)
	want := "Done.Some final answer\nExample XML: <result>value</result>"
	if got != want {
		t.Fatalf("unexpected sanitize result for mixed leaked wrapper + xml example: %q", got)
	}
}
