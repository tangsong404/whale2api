package assistantturn

import (
	"testing"

	"whale2api/internal/toolcall"
)

func TestShouldRetryIncompleteAssistantTurn_Empty(t *testing.T) {
	turn := Turn{Thinking: "reasoning only"}
	if !ShouldRetryIncompleteAssistantTurn(turn, true, 0, 1) {
		t.Fatal("expected empty-text retry")
	}
}

func TestShouldRetryIncompleteAssistantTurn_MissingToolCallsWhenToolsAvailable(t *testing.T) {
	turn := Turn{Text: "让我先找 TODO.MD 文件，看看 P0-1 具体是什么任务。"}
	if !ShouldRetryIncompleteAssistantTurn(turn, true, 0, 1) {
		t.Fatal("expected retry when tools available but no tool_calls")
	}
	if ShouldRetryIncompleteAssistantTurn(turn, false, 0, 1) {
		t.Fatal("must not retry narrative-only when request has no tools")
	}
}

func TestShouldRetryIncompleteAssistantTurn_NoRetryWithExistingCalls(t *testing.T) {
	turn := Turn{
		Text:      "reading",
		ToolCalls: []toolcall.ParsedToolCall{{Name: "Read"}},
	}
	if ShouldRetryIncompleteAssistantTurn(turn, true, 0, 1) {
		t.Fatal("must not retry when tool_calls already present")
	}
}

func TestShouldRetryEmptyOutput_StillEmptyOnly(t *testing.T) {
	if !ShouldRetryEmptyOutput(Turn{}, 0, 1) {
		t.Fatal("expected empty retry")
	}
	if ShouldRetryEmptyOutput(Turn{Text: "先找到源码。"}, 0, 1) {
		t.Fatal("ShouldRetryEmptyOutput must stay empty-only (toolsAvailable=false)")
	}
}
