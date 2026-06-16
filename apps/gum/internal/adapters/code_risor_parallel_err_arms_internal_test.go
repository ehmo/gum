package adapters

import (
	"context"
	"strings"
	"testing"
)

// TestBuildParallelFnNoArgsReturnsInvalidArgs pins buildParallelFn's
// `len(args) == 0 → INVALID_ARGS` arm (code_risor_parallel.go:46-48).
// Reached when Risor invokes gum_parallel() with zero positional
// arguments. The closure rejects upfront so the parseParallelInput
// path isn't reached on a nil shape.
func TestBuildParallelFnNoArgsReturnsInvalidArgs(t *testing.T) {
	mock := &whiteboxMockDispatcher{}
	fn := buildParallelFn(context.Background(), mock, false, false)
	got, err := fn() // zero args
	if err == nil {
		t.Fatalf("fn() err=nil, got=%+v; want INVALID_ARGS", got)
	}
	if !strings.Contains(err.Error(), "expected a list of {op, args} entries") {
		t.Errorf("err=%q; want INVALID_ARGS guidance about element shape", err.Error())
	}
}

// TestBuildParallelFnNilDispatcherReturnsInvalidArgs pins
// buildParallelFn's `disp == nil → INVALID_ARGS` arm
// (code_risor_parallel.go:42-44). Reached when the closure was built
// without a dispatcher (mis-wired execution context). The error names
// the missing dependency so operators can locate the wiring bug.
func TestBuildParallelFnNilDispatcherReturnsInvalidArgs(t *testing.T) {
	fn := buildParallelFn(context.Background(), nil, false, false)
	got, err := fn([]any{map[string]any{"op": "op.x"}})
	if err == nil {
		t.Fatalf("fn(_) err=nil, got=%+v; want INVALID_ARGS for nil dispatcher", got)
	}
	if !strings.Contains(err.Error(), "not wired") {
		t.Errorf("err=%q; want 'not wired' message", err.Error())
	}
}
