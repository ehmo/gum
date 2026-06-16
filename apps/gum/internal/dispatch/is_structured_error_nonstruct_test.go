package dispatch

import (
	"errors"
	"testing"
)

// TestIsStructuredErrorPlainErrorReturnsFalse pins the
// `!errors.As → return false` arm. IsStructuredError is the kernel's
// shorthand for "is THIS error of THIS code"; a plain error (e.g. an
// io.EOF surfaced from a probe) MUST cleanly return false rather than
// panic on the nil *StructuredError after errors.As fails.
func TestIsStructuredErrorPlainErrorReturnsFalse(t *testing.T) {
	if IsStructuredError(errors.New("plain"), ErrCodeOpNotFound) {
		t.Error("IsStructuredError(plain err, any code) = true; want false")
	}
	// nil err also must return false (errors.As(nil, &x) = false).
	if IsStructuredError(nil, ErrCodeOpNotFound) {
		t.Error("IsStructuredError(nil, any code) = true; want false")
	}
}
