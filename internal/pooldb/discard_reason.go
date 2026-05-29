package pooldb

import "strings"

// Discard reason values stored on pool_accounts.discard_reason.
const (
	DiscardReasonNone   = ""
	DiscardReasonManual = "manual"
	DiscardReasonMuted  = "muted"
	DiscardReasonBanned = "banned"
)

// PoolStatusLabel returns a UI label for pool account state.
func PoolStatusLabel(discarded bool, reason string) string {
	if !discarded {
		return "可用"
	}
	switch strings.TrimSpace(reason) {
	case DiscardReasonMuted:
		return "作废/禁言"
	case DiscardReasonBanned:
		return "作废/封号"
	default:
		return "已作废"
	}
}
