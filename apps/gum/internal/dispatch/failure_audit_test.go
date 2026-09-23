// Spec §11 acceptance: "every dispatch is appended to audit.jsonl" covers
// failures. A refused destructive call, an AUTH_REQUIRED and an upstream 403
// each append exactly one row, and the row names the resolved error code.
// dispatch/audit.go owns appendFailureAudit; Dispatch calls it once.

package dispatch_test

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// failingAdapter returns a fixed error from every Execute call.
type failingAdapter struct{ err error }

func (a failingAdapter) Execute(context.Context, *dispatch.Invocation, *dispatch.ResolvedVariant, *dispatch.Credentials) (*dispatch.Response, error) {
	return nil, a.err
}

// panickingCodeAdapter panics instead of returning, standing in for a nil
// deref inside a generated stub.
type panickingCodeAdapter struct{}

func (panickingCodeAdapter) Execute(context.Context, *dispatch.Invocation, *dispatch.ResolvedVariant, *dispatch.Credentials) (*dispatch.Response, error) {
	panic("adapter exploded mid-call")
}

// dispatchWithAdapter runs one gum.code invocation against adapter and returns
// the audit rows plus the dispatch error.
func dispatchWithAdapter(t *testing.T, adapter dispatch.Adapter, inv *dispatch.Invocation) ([]map[string]any, error) {
	t.Helper()
	sink := &recordingAuditSink{}
	disp := dispatch.NewDispatcherWithConfig(loadKernelCatalog(t), map[string]dispatch.Adapter{
		"code.risor": adapter,
	}, dispatch.DispatcherConfig{Audit: sink})
	_, err := disp.Dispatch(context.Background(), inv)
	return sink.entries, err
}

// codeInvocation is the shared gum.code invocation the failure tests dispatch.
func codeInvocation() *dispatch.Invocation {
	return &dispatch.Invocation{
		OpID:   "gum.code",
		Args:   map[string]any{"language": "risor", "source": `gum_print("failure_audit")`},
		Format: "json",
		Caller: dispatch.CallerMCP,
	}
}

// TestFailedDispatchAppendsAuditEntry is the bead repro: a recording sink and
// an adapter that always errors used to observe zero rows.
func TestFailedDispatchAppendsAuditEntry(t *testing.T) {
	entries, err := dispatchWithAdapter(t, failingAdapter{
		err: dispatch.NewStructuredError(dispatch.ErrCodeServiceDown, "upstream 503"),
	}, codeInvocation())
	if err == nil {
		t.Fatal("Dispatch returned nil error; the adapter always fails")
	}

	if len(entries) != 1 {
		t.Fatalf("audit rows = %d; want 1 for one failed dispatch", len(entries))
	}
	e := entries[0]
	for _, k := range []string{"op_id", "variant_id", "args_hash", "client_id", "risk_class", "risk_override", "error_code"} {
		if _, ok := e[k]; !ok {
			t.Errorf("failure row missing required §11 key %q (row=%v)", k, e)
		}
	}
	if got, _ := e["op_id"].(string); got != "gum.code" {
		t.Errorf("op_id = %q; want gum.code", got)
	}
	if got, _ := e["client_id"].(string); got != "mcp" {
		t.Errorf("client_id = %q; want mcp", got)
	}
	if got, _ := e["args_hash"].(string); len(got) != 64 {
		t.Errorf("args_hash = %q; want 64-char SHA-256 hex", got)
	}
	if _, present := e["panic"]; present {
		t.Errorf("failure row carries panic on an ordinary error; row=%v", e)
	}
}

// TestFailedDispatchAuditNamesResolvedCode pins the point of the new key: two
// different failures must be distinguishable in the log without joining the
// row against the gain ledger.
func TestFailedDispatchAuditNamesResolvedCode(t *testing.T) {
	for _, code := range []dispatch.ErrorCode{
		dispatch.ErrCodeAuthRequired,
		dispatch.ErrCodeInvalidArgs,
		dispatch.ErrCodeServiceDown,
	} {
		t.Run(string(code), func(t *testing.T) {
			entries, err := dispatchWithAdapter(t, failingAdapter{
				err: dispatch.NewStructuredError(code, "refused"),
			}, codeInvocation())
			if err == nil {
				t.Fatal("Dispatch returned nil error; the adapter always fails")
			}
			if len(entries) != 1 {
				t.Fatalf("audit rows = %d; want 1", len(entries))
			}
			if got, _ := entries[0]["error_code"].(string); got != string(code) {
				t.Errorf("error_code = %q; want %q", got, code)
			}
		})
	}
}

// TestUnsanitizedFailureWritesOneRowWithBothFacts is the double-write guard.
// The bypass flag used to travel on a row of its own, so a bypassed failure
// logged one row and an ordinary failure logged none.
func TestUnsanitizedFailureWritesOneRowWithBothFacts(t *testing.T) {
	inv := codeInvocation()
	inv.Caller = dispatch.CallerCLI
	inv.SkipErrorSanitizer = true

	entries, err := dispatchWithAdapter(t, failingAdapter{
		err: dispatch.NewStructuredError(dispatch.ErrCodeInvalidArgs, "upstream rejected value"),
	}, inv)
	if err == nil {
		t.Fatal("Dispatch returned nil error; the adapter always fails")
	}

	if len(entries) != 1 {
		t.Fatalf("audit rows = %d; want 1 row carrying both facts", len(entries))
	}
	if got, _ := entries[0]["sanitizer_bypassed"].(bool); !got {
		t.Errorf("sanitizer_bypassed = %v; want true (row=%v)", entries[0]["sanitizer_bypassed"], entries[0])
	}
	if got, _ := entries[0]["error_code"].(string); got != string(dispatch.ErrCodeInvalidArgs) {
		t.Errorf("error_code = %q; want INVALID_ARGS", got)
	}
}

// TestAdapterPanicWritesExactlyOneAuditRow keeps the §3.1 step 7 panic row and
// the §11 failure row from both firing for one call. The panic row is the
// dispatch's row, so it carries the code the caller sees.
func TestAdapterPanicWritesExactlyOneAuditRow(t *testing.T) {
	entries, err := dispatchWithAdapter(t, panickingCodeAdapter{}, codeInvocation())
	if err == nil {
		t.Fatal("Dispatch returned nil error after an adapter panic")
	}

	if len(entries) != 1 {
		t.Fatalf("audit rows = %d; want 1 for one panicking dispatch (rows=%v)", len(entries), entries)
	}
	if got, _ := entries[0]["panic"].(bool); !got {
		t.Errorf("panic = %v; want true (row=%v)", entries[0]["panic"], entries[0])
	}
	if got, _ := entries[0]["error_code"].(string); got != string(dispatch.ErrCodeServiceDown) {
		t.Errorf("error_code = %q; want SERVICE_DOWN", got)
	}
}

// TestSuccessfulDispatchAuditOmitsErrorCode is the negative complement: the
// key must be absent on a success row, not emitted empty.
func TestSuccessfulDispatchAuditOmitsErrorCode(t *testing.T) {
	entries, err := dispatchWithAdapter(t, adapters.NewCodeRunner(), codeInvocation())
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit rows = %d; want 1", len(entries))
	}
	if _, present := entries[0]["error_code"]; present {
		t.Errorf("success row carries error_code: %v", entries[0])
	}
}
