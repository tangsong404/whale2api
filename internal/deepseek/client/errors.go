package client

import "fmt"

type FailureKind string

const (
	FailureUnknown             FailureKind = ""
	FailureDirectUnauthorized  FailureKind = "direct_unauthorized"
	FailureManagedUnauthorized FailureKind = "managed_unauthorized"
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
