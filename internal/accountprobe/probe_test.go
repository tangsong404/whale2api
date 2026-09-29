package accountprobe

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"whale2api/internal/auth"
	"whale2api/internal/config"
	dsclient "whale2api/internal/deepseek/client"
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
	if !r.OK || r.Message != "available" || r.Token != "saved-token" {
		t.Fatalf("unexpected probeOK result: %+v", r)
	}
	if r.PoolStatus != "active" || r.AutoDiscard {
		t.Fatalf("transport-tolerant ok must not discard: %+v", r)
	}
}

func TestProbeDeviceHarvestUnavailableIsFailure(t *testing.T) {
	harvestErr := fmt.Errorf("%w: Post \"http://127.0.0.1:8090/harvest\": dial tcp 127.0.0.1:8090: connect: connection refused", auth.ErrDeviceHarvestUnavailable)
	// Guard the fixture: this message shape would be swallowed by the transient path today.
	if !poolaccounthealth.IsTransientProbeError(harvestErr.Error()) {
		t.Fatalf("fixture no longer looks transient: %v", harvestErr)
	}

	res := ProbeWithLogin(context.Background(), nil, func(context.Context, config.Account) (string, error) {
		return "", harvestErr
	}, config.Account{Email: "a@example.com"}, "")

	if res.OK {
		t.Fatalf("harvest outage must not report the account available: %+v", res)
	}
	if res.PoolStatus != "" || res.DiscardReason != "" || res.AutoDiscard {
		t.Fatalf("harvest outage must not change pool state: %+v", res)
	}
	if !strings.Contains(res.Message, "device harvest service unavailable") {
		t.Fatalf("message = %q", res.Message)
	}
}

func TestProbeNoUsableDeviceTokenIsFailure(t *testing.T) {
	inner := &dsclient.RequestFailure{Op: "login", Kind: dsclient.FailureDeviceRejected, Message: "RISK_DEVICE_DETECTED"}
	terminalErr := fmt.Errorf("%w: %w", auth.ErrNoUsableDeviceToken, inner)

	res := ProbeWithLogin(context.Background(), nil, func(context.Context, config.Account) (string, error) {
		return "", terminalErr
	}, config.Account{Email: "a@example.com"}, "")

	if res.OK {
		t.Fatalf("terminal device rejection must not report available: %+v", res)
	}
	if res.PoolStatus != "" || res.DiscardReason != "" || res.AutoDiscard {
		t.Fatalf("device rejection must not change pool state: %+v", res)
	}
	if !strings.Contains(res.Message, "no usable device token") {
		t.Fatalf("message lost the reason: %q", res.Message)
	}
}

func TestProbeDeviceRejectedErrorIsFailure(t *testing.T) {
	res := ProbeWithLogin(context.Background(), nil, func(context.Context, config.Account) (string, error) {
		return "", &dsclient.RequestFailure{Op: "login", Kind: dsclient.FailureDeviceRejected, Message: "RISK_DEVICE_DETECTED"}
	}, config.Account{Email: "a@example.com"}, "")

	if res.OK {
		t.Fatalf("device rejection must not report available: %+v", res)
	}
	if res.PoolStatus != "" || res.AutoDiscard {
		t.Fatalf("device rejection must not be classified as mute/ban: %+v", res)
	}
	if !strings.Contains(res.Message, "RISK_DEVICE_DETECTED") {
		t.Fatalf("message = %q", res.Message)
	}
}
