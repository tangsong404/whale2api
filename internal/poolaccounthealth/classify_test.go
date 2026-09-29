package poolaccounthealth

import (
	"io"
	"testing"

	"whale2api/internal/pooldb"
)

func TestClassifyLoginErrorMuted(t *testing.T) {
	if got := ClassifyLoginError("login failed: user is muted"); got != pooldb.DiscardReasonMuted {
		t.Fatalf("got %q want muted", got)
	}
}

func TestIsIncompleteResponseEOF(t *testing.T) {
	if !IsIncompleteResponse(io.ErrUnexpectedEOF) {
		t.Fatal("expected incomplete for ErrUnexpectedEOF")
	}
}

func TestIsTransientProbeErrorTLSHandshakeTimeout(t *testing.T) {
	msg := `Post "https://chat.deepseek.com/api/v0/users/login": net/http: TLS handshake timeout`
	if !IsTransientProbeError(msg) {
		t.Fatal("expected TLS handshake timeout to be transient")
	}
}

func TestClassifyResponseBytesBanned(t *testing.T) {
	raw := []byte(`{"data":{"biz_code":6,"biz_msg":"account banned"}}`)
	reason, _ := ClassifyResponseBytes(raw)
	if reason != pooldb.DiscardReasonBanned {
		t.Fatalf("got %q want banned", reason)
	}
}

func TestClassifyLoginErrorIgnoresDeviceRisk(t *testing.T) {
	cases := []string{
		"login: RISK_DEVICE_DETECTED",
		"login failed: risk_device_detected",
		"no usable device token: DeepSeek device risk control rejected the fingerprint twice: login: RISK_DEVICE_DETECTED",
	}
	for _, msg := range cases {
		if got := ClassifyLoginError(msg); got != "" {
			t.Fatalf("device risk %q classified as %q, want no discard", msg, got)
		}
	}
}

func TestClassifyResponseMapIgnoresDeviceRisk(t *testing.T) {
	resp := map[string]any{
		"code": 0,
		"data": map[string]any{"biz_code": 11, "biz_msg": "RISK_DEVICE_DETECTED"},
	}
	reason, msg := ClassifyResponseMap(resp)
	if reason != "" || msg != "" {
		t.Fatalf("device risk classified as %q/%q, want none", reason, msg)
	}
}

func TestIsDeviceRiskMessage(t *testing.T) {
	if !IsDeviceRiskMessage("RISK_DEVICE_DETECTED") {
		t.Fatal("expected device risk signal")
	}
	if IsDeviceRiskMessage("PASSWORD_OR_USER_NAME_IS_WRONG") {
		t.Fatal("password failure must not be a device risk signal")
	}
	if !IsDeviceRiskBizCode(11) || IsDeviceRiskBizCode(2) {
		t.Fatal("unexpected device risk biz code mapping")
	}
}
