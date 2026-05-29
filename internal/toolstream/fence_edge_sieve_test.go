package toolstream

import (
	"strings"
	"testing"
)

// 波浪线围栏内的工具调用标签不应触发工具调用
func TestProcessToolSieveTildeFenceDoesNotTriggerToolCall(t *testing.T) {
	var state State
	chunks := []string{
		"示例：\n~~~xml\n",
		"<tool_calls><invoke name=\"read_file\"><parameter name=\"path\">README.md</parameter></invoke></tool_calls>\n",
		"~~~\n",
		"完毕。",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected tilde-fenced tool example to stay text, got %d tool calls", toolCalls)
	}
	if !strings.Contains(textContent.String(), "示例") || !strings.Contains(textContent.String(), "完毕") {
		t.Fatalf("expected surrounding text preserved, got %q", textContent.String())
	}
}

// 4 反引号嵌套 3 反引号（内含工具标签）不应触发
func TestProcessToolSieveNestedFourBacktickFenceDoesNotTrigger(t *testing.T) {
	var state State
	input := "说明：\n````xml\n```\n<tool_calls><invoke name=\"read_file\"><parameter name=\"path\">x</parameter></invoke></tool_calls>\n```\n````\n结束。"
	chunks := strings.SplitAfter(input, "\n")
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected 4-backtick fenced example to stay text, got %d tool calls", toolCalls)
	}
}
