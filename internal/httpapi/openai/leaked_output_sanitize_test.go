package openai

import (
	"strings"
	"testing"
)

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

func TestSanitizeLeakedOutputRemovesRoleSegMarkers(t *testing.T) {
	raw := "A::sys::B::/sys::::user::C::asst::D::/asst::::tool::E::/tool::F"
	got := sanitizeLeakedOutput(raw)
	if got != "ABCDEF" {
		t.Fatalf("unexpected sanitize result for role seg markers: %q", got)
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

func TestSanitizeLeakedOutputRemovesCompleteColonToolCallWrapper(t *testing.T) {
	raw := "前置文本\n::tc::\n  ::invoke name=\"WebSearch\"::\n    ::param name=\"query\"::[[x]]::/param::\n  ::/invoke::\n::/tc::\n后置文本"
	got := sanitizeLeakedOutput(raw)
	if got != "前置文本\n\n后置文本" {
		t.Fatalf("unexpected sanitize result for leaked colon wrapper: %q", got)
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

func TestSanitizeLeakedOutputRemovesPrivateContextToolTranscriptEcho(t *testing.T) {
	raw := "前文\nツール: [tool_call_id=call_f1a338017c2b468baa08b78c4ae4f02b] Successfully modified file: taikongtu/bk_avdk/components/bk_player/core/bk_player.c\n\nツール: [tool_call_id=call_9639d25d54a54a6480df00b601926f51] Successfully modified file: taikongtu/bk_avdk/components/bk_player/core/bk_player.c\n后文"
	got := sanitizeLeakedOutput(raw)
	if strings.Contains(got, "tool_call_id=") || strings.Contains(got, "Successfully modified file") || strings.Contains(got, "ツール:") {
		t.Fatalf("expected private-context tool echo stripped, got %q", got)
	}
	if !strings.Contains(got, "前文") || !strings.Contains(got, "后文") {
		t.Fatalf("expected surrounding text preserved, got %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesMultilinePrivateContextToolEcho(t *testing.T) {
	raw := "ok\nツール:\n[name=StrReplace tool_call_id=call_abc123]\nSuccessfully modified file: a.c\nnext"
	got := sanitizeLeakedOutput(raw)
	if strings.Contains(got, "tool_call_id=") || strings.Contains(got, "Successfully modified") {
		t.Fatalf("expected multiline private-context tool echo stripped, got %q", got)
	}
	if !strings.Contains(got, "ok") || !strings.Contains(got, "next") {
		t.Fatalf("expected surrounding text preserved, got %q", got)
	}
}

func TestSanitizeLeakedOutputRemovesTruncatedColonToolCallClose(t *testing.T) {
	raw := "前置\n::tc:: ::invoke name=\"edit\":: ::param name=\"file_path\"::[[a.c]]::/param:: ::/invoke:: ::/tc:\n后置"
	got := sanitizeLeakedOutput(raw)
	if strings.Contains(got, "::tc::") || strings.Contains(got, "invoke") || strings.Contains(got, "::/tc") {
		t.Fatalf("expected truncated ::/tc: tool block stripped, got %q", got)
	}
	if !strings.Contains(got, "前置") || !strings.Contains(got, "后置") {
		t.Fatalf("expected surrounding text preserved, got %q", got)
	}
}
