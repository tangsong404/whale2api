package poolaccounthealth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"whale2api/internal/pooldb"
	"whale2api/internal/util"
)

// PoolStatusTransport marks failures from truncated HTTP/SSE bodies (not mute/ban).
const PoolStatusTransport = "transport"

// IncompleteResponseUserMessage is shown when upstream closes before the full body arrives.
const IncompleteResponseUserMessage = "上游响应不完整（连接中断，非禁言/封号）"

// IsIncompleteResponse reports EOF-style read/decode failures (truncated gzip/JSON/SSE).
func IsIncompleteResponse(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unexpected eof") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "tls:") && strings.Contains(s, "closed")
}

// IsIncompleteResponseMessage matches the same signals in a plain error string.
func IsIncompleteResponseMessage(msg string) bool {
	return IsIncompleteResponse(errors.New(strings.TrimSpace(msg)))
}

// IsTransientProbeError reports network/TLS/timeouts during probe that should not mark account failed.
func IsTransientProbeError(msg string) bool {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return false
	}
	if IsIncompleteResponseMessage(msg) {
		return true
	}
	lower := strings.ToLower(msg)
	transient := []string{
		"tls handshake timeout",
		"handshake timeout",
		"i/o timeout",
		"context deadline exceeded",
		"connection timed out",
		"timeout awaiting response",
		"client.timeout exceeded",
		"exceeded while awaiting headers",
		"dial tcp",
		"lookup ",
		"no route to host",
		"connection reset by peer",
		"eof",
	}
	for _, needle := range transient {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// ClassifyLoginError returns a pool discard reason when a login error indicates mute or ban.
func ClassifyLoginError(msg string) string {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if isMutedSignal(lower) {
		return pooldb.DiscardReasonMuted
	}
	if isBannedSignal(lower) {
		return pooldb.DiscardReasonBanned
	}
	return ""
}

// ClassifyResponseBytes inspects a raw HTTP body (JSON or text) for mute/ban signals.
func ClassifyResponseBytes(raw []byte) (reason, message string) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", ""
	}
	if trimmed[0] == '{' {
		var envelope map[string]any
		if err := json.Unmarshal(raw, &envelope); err == nil {
			return ClassifyResponseMap(envelope)
		}
	}
	lower := strings.ToLower(trimmed)
	if isMutedSignal(lower) {
		return pooldb.DiscardReasonMuted, "user is muted"
	}
	if isBannedSignal(lower) {
		return pooldb.DiscardReasonBanned, "account banned or disabled"
	}
	return "", ""
}

// ClassifyResponseMap inspects a DeepSeek JSON envelope for mute/ban signals.
func ClassifyResponseMap(resp map[string]any) (reason, message string) {
	if resp == nil {
		return "", ""
	}
	data, _ := resp["data"].(map[string]any)
	bizCode := util.IntFrom(data["biz_code"])
	bizMsg, _ := data["biz_msg"].(string)
	bizData, _ := data["biz_data"].(map[string]any)
	combined := strings.ToLower(strings.TrimSpace(bizMsg))
	if msg, _ := resp["msg"].(string); strings.TrimSpace(msg) != "" {
		combined += " " + strings.ToLower(strings.TrimSpace(msg))
	}

	if isMutedEnvelope(bizCode, combined, bizData) {
		msg := strings.TrimSpace(bizMsg)
		if msg == "" {
			msg = "user is muted"
		}
		if ts := muteUntilFromBizData(bizData); ts != nil {
			msg = fmt.Sprintf("%s（解禁 %s）", msg, ts.Local().Format("2006-01-02 15:04:05"))
		}
		return pooldb.DiscardReasonMuted, msg
	}
	if isBannedSignal(combined) || isBannedBizCode(bizCode) {
		msg := strings.TrimSpace(bizMsg)
		if msg == "" {
			msg = "account banned or disabled"
		}
		return pooldb.DiscardReasonBanned, msg
	}
	return "", ""
}

func isMutedEnvelope(bizCode int, combined string, bizData map[string]any) bool {
	if isMutedSignal(combined) {
		return true
	}
	if bizCode == 5 && strings.Contains(combined, "mute") {
		return true
	}
	if bizData != nil {
		if util.IntFrom(bizData["is_muted"]) == 1 {
			return true
		}
	}
	return false
}

func isMutedSignal(s string) bool {
	return strings.Contains(s, "user is muted") ||
		strings.Contains(s, "is_muted") ||
		strings.Contains(s, "禁言") ||
		strings.Contains(s, "muted")
}

func isBannedSignal(s string) bool {
	keywords := []string{
		"banned", "ban ", "account disabled", "disabled account",
		"forbidden", "suspended", "deactivated", "封号", "封禁", "禁用",
		"account has been", "violation",
	}
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func isBannedBizCode(bizCode int) bool {
	switch bizCode {
	case 6, 7, 8, 9, 10, 403, 1001, 1002:
		return true
	default:
		return false
	}
}

func muteUntilFromBizData(bizData map[string]any) *time.Time {
	if bizData == nil {
		return nil
	}
	for _, key := range []string{"mute_until", "muted_until"} {
		if ts := unixTimeFromAny(bizData[key]); ts != nil {
			return ts
		}
	}
	return nil
}

func unixTimeFromAny(v any) *time.Time {
	switch n := v.(type) {
	case float64:
		sec := int64(n)
		if sec <= 0 {
			return nil
		}
		t := time.Unix(sec, 0)
		return &t
	case int:
		if n <= 0 {
			return nil
		}
		t := time.Unix(int64(n), 0)
		return &t
	case int64:
		if n <= 0 {
			return nil
		}
		t := time.Unix(n, 0)
		return &t
	default:
		return nil
	}
}
