package dispatch

import (
	"errors"
	"testing"
)

// The two §11 failure-audit helpers are defensive: Dispatch runs
// wrapKernelError first, so every error they see already carries a
// *StructuredError. These cover the arm that does not.

func TestStructuredErrorCodeReturnsEmptyForAPlainError(t *testing.T) {
	if got := structuredErrorCode(errors.New("plain")); got != "" {
		t.Errorf("structuredErrorCode = %q; want the empty code", got)
	}
	if got := structuredErrorCode(nil); got != "" {
		t.Errorf("structuredErrorCode(nil) = %q; want the empty code", got)
	}
}

func TestDispatchAuditedIsFalseForAPlainError(t *testing.T) {
	if dispatchAudited(errors.New("plain")) {
		t.Error("dispatchAudited = true for an error carrying no *StructuredError")
	}
	if dispatchAudited(nil) {
		t.Error("dispatchAudited(nil) = true; want false")
	}
	if dispatchAudited(NewStructuredError(ErrCodeServiceDown, "x")) {
		t.Error("dispatchAudited = true for an unmarked *StructuredError")
	}
}
