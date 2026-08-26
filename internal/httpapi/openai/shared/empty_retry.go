package shared

import "strings"

const EmptyOutputRetrySuffix = "The previous reply had no visible output. Regenerate a visible final answer or a tool call."

// MissingToolCallRetrySuffix is appended when tools were available but the turn
// produced no parseable tool calls (e.g. narrative-only "I'll read the file").
const MissingToolCallRetrySuffix = "No tool call was made in this turn. " +
	"Decide whether the task is truly complete (not an imaginary or narrative-only completion). " +
	"Example: claiming a file was edited/written without an Edit / Write / StrReplace (or similar) write tool call is imaginary. " +
	"If the user says they already made a manual change, treat the task as temporarily finished. " +
	"If the task truly cannot be completed, you may stop. " +
	"If incomplete, immediately supply a complete tool-call format. " +
	"Do not rephrase, re-greet, re-explain, or restate the previous assistant reply. " +
	"User-facing prose must use the user's language, or the language the user explicitly requested — not the language of these instructions."

func EmptyOutputRetryEnabled() bool {
	return true
}

func EmptyOutputRetryMaxAttempts() int {
	return 1
}

// ClonePayloadForEmptyOutputRetry creates a retry payload with the empty-output
// suffix. Prefer ClonePayloadForAssistantRetry when the suffix depends on why
// the turn is being retried.
func ClonePayloadForEmptyOutputRetry(payload map[string]any, parentMessageID int) map[string]any {
	return ClonePayloadForAssistantRetry(payload, parentMessageID, EmptyOutputRetrySuffix)
}

// ClonePayloadForAssistantRetry creates a retry payload with the given suffix
// appended and, if parentMessageID > 0, sets parent_message_id so the retry is
// submitted as a follow-up turn in the same DeepSeek session.
func ClonePayloadForAssistantRetry(payload map[string]any, parentMessageID int, suffix string) map[string]any {
	clone := make(map[string]any, len(payload))
	for k, v := range payload {
		clone[k] = v
	}
	original, _ := payload["prompt"].(string)
	clone["prompt"] = AppendRetrySuffix(original, suffix)
	if parentMessageID > 0 {
		clone["parent_message_id"] = parentMessageID
	}
	return clone
}

// RetrySuffixForTurn picks the retry prompt suffix: empty-output vs missing tool call.
func RetrySuffixForTurn(visibleText string, toolsAvailable bool) string {
	if strings.TrimSpace(visibleText) == "" {
		return EmptyOutputRetrySuffix
	}
	if toolsAvailable {
		return MissingToolCallRetrySuffix
	}
	return EmptyOutputRetrySuffix
}

// RetryReasonLabel is a short log label for why a synthetic retry was triggered.
func RetryReasonLabel(visibleText string, toolsAvailable bool) string {
	if strings.TrimSpace(visibleText) == "" {
		return "empty_output"
	}
	if toolsAvailable {
		return "missing_tool_calls"
	}
	return "empty_output"
}

func AppendEmptyOutputRetrySuffix(prompt string) string {
	return AppendRetrySuffix(prompt, EmptyOutputRetrySuffix)
}

func AppendRetrySuffix(prompt, suffix string) string {
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		suffix = EmptyOutputRetrySuffix
	}
	prompt = strings.TrimRight(prompt, "\r\n\t ")
	if prompt == "" {
		return suffix
	}
	return prompt + "\n\n" + suffix
}

func UsagePromptWithEmptyOutputRetry(originalPrompt string, retryAttempts int) string {
	return UsagePromptWithRetrySuffix(originalPrompt, retryAttempts, EmptyOutputRetrySuffix)
}

func UsagePromptWithRetrySuffix(originalPrompt string, retryAttempts int, suffix string) string {
	if retryAttempts <= 0 {
		return originalPrompt
	}
	parts := make([]string, 0, retryAttempts+1)
	parts = append(parts, originalPrompt)
	next := originalPrompt
	for i := 0; i < retryAttempts; i++ {
		next = AppendRetrySuffix(next, suffix)
		parts = append(parts, next)
	}
	return strings.Join(parts, "\n")
}
