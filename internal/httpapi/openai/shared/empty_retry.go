package shared

import "strings"

const EmptyOutputRetrySuffix = "前回の返信に可視の出力がなかった。可視の最終回答またはツール呼び出しを再生成すること。"

// MissingToolCallRetrySuffix is appended when tools were available but the turn
// produced no parseable tool calls (e.g. narrative-only "I'll read the file").
const MissingToolCallRetrySuffix = "本ターンではツール呼び出しが行われなかった。" +
	"タスクが本当に完了したか（空想・叙述上の完了ではないか）を判断すること。" +
	"例：ファイルを編集・書き込んだと宣言したのに Edit / Write / StrReplace 等の書き込み系ツール呼び出しがなければ、完了は空想である。" +
	"未完了なら、直ちに完全なツール呼び出し形式で補うこと。"

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
