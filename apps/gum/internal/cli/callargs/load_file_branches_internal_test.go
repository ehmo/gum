package callargs

import (
	"strings"
	"testing"
)

// TestLoadFileEmptyPathRejected pins the
// `path == "" → "empty @ file reference"` arm. A bare "@" with no path
// after the prefix MUST be rejected with CLI_ARG_INVALID rather than
// fall through to os.ReadFile("") which surfaces a confusing
// "open : no such file" error.
func TestLoadFileEmptyPathRejected(t *testing.T) {
	_, err := loadFile("@", nil)
	if err == nil {
		t.Fatal("want CLI_ARG_INVALID; got nil")
	}
	var ce *Error
	if !asArgError(err, &ce) {
		t.Fatalf("err type=%T; want *callargs.Error", err)
	}
	if ce.Code != "CLI_ARG_INVALID" {
		t.Errorf("Code=%q; want CLI_ARG_INVALID", ce.Code)
	}
	if !strings.Contains(ce.Reason, "empty @ file reference") {
		t.Errorf("Reason=%q; want 'empty @ file reference' substr", ce.Reason)
	}
}

// TestLoadFileReadFileErrorWrapsAsCLIArgInvalid pins the
// `os.ReadFile err → CLI_ARG_INVALID` arm. A nonexistent @path MUST be
// surfaced as a structured CLI_ARG_INVALID with the underlying syscall
// error in Reason so the operator sees both the offending arg and the
// real failure (ENOENT, EACCES, etc.) in one envelope.
func TestLoadFileReadFileErrorWrapsAsCLIArgInvalid(t *testing.T) {
	_, err := loadFile("@/does/not/exist/whatsoever.json", nil)
	if err == nil {
		t.Fatal("want CLI_ARG_INVALID; got nil")
	}
	var ce *Error
	if !asArgError(err, &ce) {
		t.Fatalf("err type=%T; want *callargs.Error", err)
	}
	if ce.Code != "CLI_ARG_INVALID" {
		t.Errorf("Code=%q; want CLI_ARG_INVALID", ce.Code)
	}
	if ce.Reason == "" {
		t.Error("Reason empty; want syscall err message")
	}
}

// asArgError is a local errors.As shim — keeps the test file self-
// contained and avoids importing errors for one call.
func asArgError(err error, target **Error) bool {
	if e, ok := err.(*Error); ok {
		*target = e
		return true
	}
	return false
}
