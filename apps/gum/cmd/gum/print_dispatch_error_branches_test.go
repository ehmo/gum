package main

import (
	"bytes"
	"errors"
	"testing"
)

// TestPrintDispatchErrorPlainErrorPassesThrough pins the
// `!errors.As(err, &se) → return err` arm. printDispatchError is the
// CLI's terminal error-rendering shim; a plain non-structured error
// (e.g. an io.EOF surfaced from an adapter) MUST pass through
// unchanged so cobra prints it as a normal error rather than crash on
// nil StructuredError dereferences in the envelope renderer.
func TestPrintDispatchErrorPlainErrorPassesThrough(t *testing.T) {
	var buf bytes.Buffer
	sentinel := errors.New("plain non-structured error")

	got := printDispatchError(&buf, "read", sentinel)
	if got != sentinel {
		t.Errorf("returned err=%v; want sentinel pass-through", got)
	}
	if buf.Len() != 0 {
		t.Errorf("buf=%q; want empty (no envelope rendered for non-structured err)", buf.String())
	}
}
