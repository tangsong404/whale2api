package client

import (
	"errors"
	"fmt"
)

type FailureKind string

const (
	FailureUnknown             FailureKind = ""
	FailureDirectUnauthorized  FailureKind = "direct_unauthorized"
	FailureManagedUnauthorized FailureKind = "managed_unauthorized"
	// FailureDeviceRejected means DeepSeek risk control rejected the device fingerprint token
	// (biz_code 11 / RISK_DEVICE_DETECTED); re-harvesting the token may fix it.
	FailureDeviceRejected FailureKind = "device_rejected"
)

type RequestFailure struct {
	Op      string
	Kind    FailureKind
	Message string
}

func (e *RequestFailure) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Op != "" && e.Message != "":
		return fmt.Sprintf("%s: %s", e.Op, e.Message)
	case e.Op != "":
		return e.Op + " failed"
	case e.Message != "":
		return e.Message
	default:
		return "request failed"
	}
}

// DeviceRejected reports whether this failure is a device-fingerprint rejection.
// It is intentionally a consumer-defined interface so callers outside this package
// (e.g. internal/auth) can detect the condition without importing client.
func (e *RequestFailure) DeviceRejected() bool {
	return e != nil && e.Kind == FailureDeviceRejected
}

// IsDeviceRejectedError reports whether err (or any wrapped error) is a device rejection.
func IsDeviceRejectedError(err error) bool {
	if err == nil {
		return false
	}
	var rejected interface{ DeviceRejected() bool }
	return errors.As(err, &rejected) && rejected.DeviceRejected()
}
