package assistantturn

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"whale2api/internal/config"
)

const upstreamEmptyThinkingPreviewMaxRunes = 8000

// LogUpstreamEmptyOutputDiagnostic logs a structured diagnostic when the upstream
// completion produced no client-visible output and the error code is upstream_empty_output.
func LogUpstreamEmptyOutputDiagnostic(surface string, stream bool, phase string, turn Turn, completionPayload map[string]any, clientContentLength *int64) {
	if turn.Error == nil || turn.Error.Code != "upstream_empty_output" {
		return
	}
	reason := upstreamEmptyTriggerReason(turn)
	preview := truncateThinkingPreview(turn.Thinking, upstreamEmptyThinkingPreviewMaxRunes)

	payloadBytes := -1
	if completionPayload != nil {
		if b, err := json.Marshal(completionPayload); err == nil {
			payloadBytes = len(b)
		}
	}

	args := []any{
		"surface", surface,
		"stream", stream,
		"phase", phase,
		"trigger_reason", reason,
		"upstream_message", turn.Error.Message,
		"model", turn.Model,
		"response_message_id", turn.ResponseMessageID,
		"thinking_runes", utf8.RuneCountInString(turn.Thinking),
		"thinking_bytes", len(turn.Thinking),
		"thinking_preview", preview,
		"raw_thinking_bytes", len(turn.RawThinking),
		"detection_thinking_bytes", len(turn.DetectionThinking),
		"visible_text_bytes", len(turn.Text),
		"prompt_byte_len", len(turn.Prompt),
	}
	if payloadBytes >= 0 {
		args = append(args, "completion_payload_json_bytes", payloadBytes)
	}
	if clientContentLength != nil {
		args = append(args, "client_request_content_length", *clientContentLength)
	}
	config.Logger.Warn("[upstream_empty_output]", args...)
}

func upstreamEmptyTriggerReason(turn Turn) string {
	if turn.ContentFilter {
		return "content_filter_path"
	}
	if strings.TrimSpace(turn.Text) != "" {
		return "unexpected_nonempty_visible_text"
	}
	if len(turn.ToolCalls) > 0 {
		return "unexpected_tool_calls_present"
	}
	if strings.TrimSpace(turn.Thinking) != "" {
		return "thinking_only_no_visible_text_or_tools"
	}
	return "no_visible_text_no_thinking_no_tools"
}

func truncateThinkingPreview(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	cut := 0
	remain := maxRunes
	for cut < len(s) && remain > 0 {
		_, sz := utf8.DecodeRuneInString(s[cut:])
		if sz == 0 {
			break
		}
		cut += sz
		remain--
	}
	return s[:cut] + "…(truncated)"
}
