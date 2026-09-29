package client

import (
	"errors"
	"testing"

	"whale2api/internal/config"
)

func TestLoginPayloadUsesAccountDeviceID(t *testing.T) {
	payload, err := loginPayload(config.Account{Email: "u@example.com", Password: "pwd", DeviceID: "B-real-device-token"})
	if err != nil {
		t.Fatal(err)
	}
	if got := payload["device_id"]; got != "B-real-device-token" {
		t.Fatalf("device_id = %#v, want account token", got)
	}
	if got := payload["email"]; got != "u@example.com" {
		t.Fatalf("email = %#v", got)
	}
}

func TestLoginPayloadMissingDeviceIDDoesNotPanic(t *testing.T) {
	payload, err := loginPayload(config.Account{Email: "u@example.com", Password: "pwd"})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := payload["device_id"]; !ok || got != "" {
		t.Fatalf("device_id = %#v (present=%v), want empty string", got, ok)
	}
}

func TestLoginPayloadRequiresIdentifier(t *testing.T) {
	if _, err := loginPayload(config.Account{Password: "pwd"}); err == nil {
		t.Fatal("expected missing email/mobile error")
	}
}

func TestParseLoginResponseDeviceRisk(t *testing.T) {
	resp := map[string]any{
		"code": float64(0),
		"data": map[string]any{
			"biz_code": float64(11),
			"biz_msg":  "RISK_DEVICE_DETECTED",
		},
	}
	_, err := parseLoginResponse(resp)
	if err == nil {
		t.Fatal("expected device risk error")
	}
	if !IsDeviceRejectedError(err) {
		t.Fatalf("expected device rejection, got %v", err)
	}
	var rf *RequestFailure
	if !errors.As(err, &rf) {
		t.Fatalf("expected *RequestFailure, got %T", err)
	}
	if rf.Kind != FailureDeviceRejected || rf.Op != "login" {
		t.Fatalf("unexpected failure: %+v", rf)
	}
	if !rf.DeviceRejected() {
		t.Fatal("DeviceRejected() should be true")
	}
}

func TestParseLoginResponseDeviceRiskByMessage(t *testing.T) {
	resp := map[string]any{
		"code": float64(0),
		"data": map[string]any{
			"biz_code": float64(0),
			"biz_msg":  "risk_device_detected",
		},
	}
	_, err := parseLoginResponse(resp)
	if !IsDeviceRejectedError(err) {
		t.Fatalf("expected device rejection by message, got %v", err)
	}
}

func TestParseLoginResponsePasswordWrongIsNotDeviceRisk(t *testing.T) {
	resp := map[string]any{
		"code": float64(0),
		"data": map[string]any{
			"biz_code": float64(2),
			"biz_msg":  "PASSWORD_OR_USER_NAME_IS_WRONG",
		},
	}
	_, err := parseLoginResponse(resp)
	if err == nil {
		t.Fatal("expected login failure")
	}
	if IsDeviceRejectedError(err) {
		t.Fatalf("password failure misclassified as device risk: %v", err)
	}
}

func TestParseLoginResponseSuccess(t *testing.T) {
	resp := map[string]any{
		"code": float64(0),
		"data": map[string]any{
			"biz_code": float64(0),
			"biz_data": map[string]any{
				"user": map[string]any{"token": "upstream-token"},
			},
		},
	}
	token, err := parseLoginResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	if token != "upstream-token" {
		t.Fatalf("token = %q", token)
	}
}
