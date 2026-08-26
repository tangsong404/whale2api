package assistantturn

import "strings"

// ShouldRetryIncompleteAssistantTurn retries when:
//   - the turn produced no visible text and no tool calls (empty / thinking-only), or
//   - tools were available but the turn produced no parseable tool calls
//     (narrative-only announce / imaginary completion).
// It does not inspect wording heuristics; structural signals only.
func ShouldRetryIncompleteAssistantTurn(turn Turn, toolsAvailable bool, attempts, maxAttempts int) bool {
	if attempts >= maxAttempts || turn.ContentFilter || len(turn.ToolCalls) > 0 {
		return false
	}
	if strings.TrimSpace(turn.Text) == "" {
		return true
	}
	return toolsAvailable
}

// ShouldRetryEmptyOutput returns true when the turn produced no visible text
// and has no tool calls or content filter.
func ShouldRetryEmptyOutput(turn Turn, attempts, maxAttempts int) bool {
	return ShouldRetryIncompleteAssistantTurn(turn, false, attempts, maxAttempts)
}
