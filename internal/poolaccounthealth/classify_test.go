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
