package dispatch

import "testing"

// TestMapRateLimitedNilReturnsNil pins mapRateLimited's
// `err == nil → return nil` arm (errors.go:228-230). The translator
// is called on every dispatch outcome including the happy path, so
// passing nil MUST return nil without allocating a StructuredError.
func TestMapRateLimitedNilReturnsNil(t *testing.T) {
	t.Parallel()
	if got := mapRateLimited(nil); got != nil {
		t.Errorf("got=%v; want nil", got)
	}
}
