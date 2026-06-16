// Package dispatch — context cancellation helpers.
//
// Two consumer paths must both work: (1) stdlib callers test errors.Is(err,
// context.Canceled); (2) spec §3 envelope tests use IsStructuredError(err,
// ErrCodeCancelled). cancelledError satisfies both by embedding *StructuredError
// (path 2) and delegating Unwrap to the raw context error (path 1).
package dispatch

import "context"

// cancelledError attaches a context cause to a *StructuredError so the
// errors.Is / errors.As chain can traverse both the structured envelope
// and the underlying context error.
type cancelledError struct {
	*StructuredError
	cause error
}

// Unwrap returns the raw context error so errors.Is(err, context.Canceled) works.
func (c *cancelledError) Unwrap() error { return c.cause }

// As resolves *StructuredError targets, enabling the spec §3 envelope path.
func (c *cancelledError) As(target interface{}) bool {
	if t, ok := target.(**StructuredError); ok {
		*t = c.StructuredError
		return true
	}
	return false
}

// newCancelledError returns an error satisfying both consumer paths described
// in the package doc. cause must be context.Canceled or context.DeadlineExceeded.
func newCancelledError(cause error) error {
	return &cancelledError{
		StructuredError: NewStructuredError(ErrCodeCancelled, "operation cancelled"),
		cause:           cause,
	}
}

// checkCancelled returns a *cancelledError if ctx has been cancelled, nil otherwise.
// Call between lifecycle steps to surface cancellation before the next step begins.
func checkCancelled(ctx context.Context, _ string) error {
	if cause := ctx.Err(); cause != nil {
		return newCancelledError(cause)
	}
	return nil
}
