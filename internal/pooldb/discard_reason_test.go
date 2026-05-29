package pooldb

import "testing"

func TestPoolStatusLabel(t *testing.T) {
	if got := PoolStatusLabel(false, ""); got != "可用" {
		t.Fatalf("active: got %q", got)
	}
	if got := PoolStatusLabel(true, DiscardReasonMuted); got != "作废/禁言" {
		t.Fatalf("muted: got %q", got)
	}
	if got := PoolStatusLabel(true, DiscardReasonBanned); got != "作废/封号" {
		t.Fatalf("banned: got %q", got)
	}
	if got := PoolStatusLabel(true, DiscardReasonManual); got != "已作废" {
		t.Fatalf("manual: got %q", got)
	}
}
