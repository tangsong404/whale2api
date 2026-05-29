package accountprobe

import (
	"testing"

	"whale2api/internal/poolaccounthealth"
	"whale2api/internal/pooldb"
)

func TestClassifyCompletionBodyMutedJSON(t *testing.T) {
	raw := []byte(`{"code":0,"msg":"","data":{"biz_code":5,"biz_msg":"user is muted","biz_data":{"is_muted":1,"mute_until":1779276663.438}}}`)
	kind, msg := poolaccounthealth.ClassifyResponseBytes(raw)
	if kind != pooldb.DiscardReasonMuted {
		t.Fatalf("expected muted, got %q", kind)
	}
	if msg == "" {
		t.Fatal("expected message")
	}
}

func TestClassifyCompletionBodyOKSSE(t *testing.T) {
	raw := []byte("data: {\"p\":\"response/content\",\"v\":\"hi\"}\n\ndata: [DONE]\n")
	kind, _ := poolaccounthealth.ClassifyResponseBytes(raw)
	if kind != "" {
		t.Fatalf("expected no classification for SSE, got %q", kind)
	}
}

func TestClassifyLoginFailureBanned(t *testing.T) {
	if got := poolaccounthealth.ClassifyLoginError("account has been banned"); got != pooldb.DiscardReasonBanned {
		t.Fatalf("expected banned, got %q", got)
	}
}

func TestProbeLoginTLSHandshakeTimeoutTreatedAsOK(t *testing.T) {
	errMsg := `Post "https://chat.deepseek.com/api/v0/users/login": net/http: TLS handshake timeout`
	if !poolaccounthealth.IsTransientProbeError(errMsg) {
		t.Fatal("expected transient login error")
	}
	r := probeOK("cached-token")
	if !r.OK {
		t.Fatal("expected ok")
	}
}

func TestProbeOKDespiteTransport(t *testing.T) {
	r := probeOK("saved-token")
	if !r.OK || r.Message != "可用" || r.Token != "saved-token" {
		t.Fatalf("unexpected probeOK result: %+v", r)
	}
	if r.PoolStatus != "active" || r.AutoDiscard {
		t.Fatalf("transport-tolerant ok must not discard: %+v", r)
	}
}
