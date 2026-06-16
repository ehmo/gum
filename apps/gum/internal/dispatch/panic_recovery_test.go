// Package dispatch — RED TEAM tests for panic recovery (spec.md §3.1.7, line 235).
//
// These tests are intentionally FAILING until the Green Team implements:
//   - deferred recover() in executeAdapter (or executeAdapterSafe wrapper)
//   - AuditSink interface with injection point on NewDispatcher / NewDispatcherWithAudit
//   - ERROR-level slog emission of sanitized stack trace with "stack" or "stack_trace" attribute
//
// Spec rule: spec.md §3.1 step 7 (line 235): internal/dispatch wraps every call
// to Executor.Execute in a deferred recover(). Panic → SERVICE_DOWN envelope,
// sanitized stack to slog ERROR, audit entry with panic:true. MCP server MUST NOT crash.
package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

// panickingAdapter is a minimal Adapter whose Execute always panics with the
// configured value. Used across all panic-recovery tests.
type panickingAdapter struct{ panicValue any }

func (p *panickingAdapter) Execute(ctx context.Context, inv *Invocation, rv *ResolvedVariant, creds *Credentials) (*Response, error) {
	panic(p.panicValue)
}

// panicTestCatalog returns a catalog with a single op whose default variant
// binds to adapter key "panic.test". This lets us route dispatch to panickingAdapter.
func panicTestCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{
			{
				OpID:             "gum.panic_test",
				OpSchemaVersion:  1,
				Title:            "Panic test op",
				Summary:          "Used only in panic-recovery tests.",
				DefaultVariantID: "gum.panic_test.v1",
				ServiceFamily:    "meta",
				Service:          "meta",
				Variants: []catalog.Variant{
					{
						VariantID:            "gum.panic_test.v1",
						VariantSchemaVersion: 1,
						Version:              "v1",
						Stability:            "stable",
						InterfaceKind:        "sdk-native",
						BackendKind:          "typed-rest-sdk",
						Preferred:            true,
						RiskClass:            catalog.RiskClassRead,
						AuthStrategy:         "none",
						DefaultFormat:        "json",
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           "panic.test",
							OperationKey:         "gum.panic_test.exec",
						},
					},
				},
			},
		},
	}
}

// panicTestInvocation returns a read-class Invocation for the panic test op.
func panicTestInvocation() *Invocation {
	return &Invocation{
		OpID:      "gum.panic_test",
		Args:      map[string]any{},
		Format:    "json",
		RequestID: "panic-test-req",
	}
}

// panicCapturingHandler captures slog records for assertion in tests.
// Separate from lifecycle_test.go's capturingHandler because that lives in
// package dispatch_test and is not visible here (package dispatch).
type panicCapturingHandler struct {
	records []slog.Record
}

func (h *panicCapturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *panicCapturingHandler) Handle(_ context.Context, r slog.Record) error {
	// Copy the record so that Attrs can be iterated later.
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *panicCapturingHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *panicCapturingHandler) WithGroup(name string) slog.Handler       { return h }

// ---------------------------------------------------------------------------
// Test 1: SERVICE_DOWN envelope shape
// ---------------------------------------------------------------------------

// TestDispatchAdapterPanicReturnsServiceDownEnvelope verifies that a panicking
// adapter causes Dispatch to return a *StructuredError with ErrCode==SERVICE_DOWN,
// a message containing "internal error" but with no panic or stack text, and
// Retryable==false.
//
// Spec: line 235 — "returns {"error_code":"SERVICE_DOWN","message":"internal error;
// see audit log","retryable":false,"isError":true} to the caller without crashing"
func TestDispatchAdapterPanicReturnsServiceDownEnvelope(t *testing.T) {
	// Catch any panic that escapes Dispatch — if this fires the test would
	// otherwise crash the process; we want a clean test failure instead.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Dispatch propagated panic to caller: %v", r)
		}
	}()

	snap := panicTestCatalog()
	adapter := &panickingAdapter{panicValue: fmt.Errorf("nil deref simulation")}
	disp := NewDispatcher(snap, map[string]Adapter{"panic.test": adapter})

	_, err := disp.Dispatch(context.Background(), panicTestInvocation())

	// Must return a non-nil error.
	if err == nil {
		t.Fatal("expected non-nil error from Dispatch after adapter panic, got nil")
	}

	// Error must be *StructuredError.
	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("expected error to be *StructuredError; got %T: %v", err, err)
	}

	// Code must be SERVICE_DOWN (spec line 235).
	if se.ErrCode != ErrCodeServiceDown {
		t.Errorf("expected ErrCode=SERVICE_DOWN, got %q", se.ErrCode)
	}

	// Message must contain "internal error" (spec: "internal error; see audit log").
	if !strings.Contains(se.Message, "internal error") {
		t.Errorf("expected Message to contain %q, got %q", "internal error", se.Message)
	}

	// Message must NOT contain "panic:" — no stack leakage (spec: "without crashing …
	// panic stack written to structured log … NOT in MCP response").
	if strings.Contains(se.Message, "panic:") {
		t.Errorf("Message must not contain 'panic:' (stack leakage): %q", se.Message)
	}

	// Message must NOT contain stack frame indicators.
	for _, forbidden := range []string{".go:", "goroutine ", "0x"} {
		if strings.Contains(se.Message, forbidden) {
			t.Errorf("Message must not contain stack frame text %q: %q", forbidden, se.Message)
		}
	}

	// Retryable must be false (spec line 235: "retryable: false").
	if se.Retryable != false {
		t.Errorf("expected Retryable=false, got %v", se.Retryable)
	}
}

// ---------------------------------------------------------------------------
// Test 2: repeated panics do not kill the goroutine / process
// ---------------------------------------------------------------------------

// TestDispatchAdapterPanicDoesNotKillProcess calls Dispatch 10 times against a
// panicking adapter, asserting each call returns normally (no escaping panic).
//
// Spec: line 235 — "A nil-pointer dereference in a generated REST stub MUST NOT
// terminate a long-running `gum mcp --stdio` session."
func TestDispatchAdapterPanicDoesNotKillProcess(t *testing.T) {
	outerPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				outerPanic = true
			}
		}()

		snap := panicTestCatalog()
		adapter := &panickingAdapter{panicValue: "simulated nil dereference"}
		disp := NewDispatcher(snap, map[string]Adapter{"panic.test": adapter})

		for i := range 10 {
			inv := panicTestInvocation()
			inv.RequestID = fmt.Sprintf("panic-loop-%d", i)
			_, err := disp.Dispatch(context.Background(), inv)
			if err == nil {
				t.Errorf("call %d: expected error, got nil", i)
			}
		}
	}()

	if outerPanic {
		t.Fatal("a panic escaped Dispatch to the test's outer recover — process would have crashed")
	}
}

// ---------------------------------------------------------------------------
// Test 3: stack logged at ERROR level via slog
// ---------------------------------------------------------------------------

// TestDispatchAdapterPanicLogsStackToSlog verifies that when an adapter panics,
// the dispatch layer emits at least one slog record at LevelError with an
// attribute "stack" or "stack_trace" that contains the word "runtime" (proving
// a real stack trace was captured, not a placeholder string).
//
// Spec: line 235 — "The panic stack is written to the structured log at ERROR
// level, not to the MCP response."
func TestDispatchAdapterPanicLogsStackToSlog(t *testing.T) {
	h := &panicCapturingHandler{}
	logger := slog.New(h)
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	snap := panicTestCatalog()
	adapter := &panickingAdapter{panicValue: "stack_log_test panic"}
	disp := NewDispatcher(snap, map[string]Adapter{"panic.test": adapter})

	_, _ = disp.Dispatch(context.Background(), panicTestInvocation())

	// Look for an ERROR-level record with a "stack" or "stack_trace" attribute
	// whose value contains "runtime".
	found := false
	for _, rec := range h.records {
		if rec.Level != slog.LevelError {
			continue
		}
		rec.Attrs(func(a slog.Attr) bool {
			if a.Key != "stack" && a.Key != "stack_trace" {
				return true
			}
			stackStr := a.Value.String()
			if strings.Contains(stackStr, "runtime") {
				found = true
			}
			return true
		})
		if found {
			break
		}
	}

	if !found {
		t.Error("expected slog ERROR record with attribute 'stack' or 'stack_trace' containing 'runtime'; none found — stack is not being logged per spec line 235")
	}
}

// ---------------------------------------------------------------------------
// Test 4: audit entry contains required fields including panic:true
// ---------------------------------------------------------------------------

// AuditSink is the Red Team contract for an audit entry sink.
//
// The Green Team MUST add this interface to the dispatch package and wire it
// into NewDispatcher (or a new NewDispatcherWithAudit constructor). Until that
// happens, tests referencing AuditSink will fail at compile time, which is the
// intended signal.
//
// Spec: line 235 — "appends an audit-log entry carrying risk_class, op_id,
// variant_id, args_hash, and panic: true"
type AuditSink interface {
	Append(entry map[string]any)
}

// testAuditSink captures Append calls for assertion.
type testAuditSink struct {
	entries []map[string]any
}

func (s *testAuditSink) Append(entry map[string]any) {
	// Deep copy so mutations after the call don't corrupt captured data.
	cp := make(map[string]any, len(entry))
	for k, v := range entry {
		cp[k] = v
	}
	s.entries = append(s.entries, cp)
}

// TestDispatchAdapterPanicAuditEntryHasPanicTrue wires a testAuditSink via
// NewDispatcherWithAudit (a constructor the Green Team must add), triggers a
// panic, and asserts the captured audit entry contains the required fields.
//
// This test FAILS at compile time until NewDispatcherWithAudit is added to the
// dispatch package.
func TestDispatchAdapterPanicAuditEntryHasPanicTrue(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Dispatch propagated panic during audit test: %v", r)
		}
	}()

	snap := panicTestCatalog()
	adapter := &panickingAdapter{panicValue: errors.New("audit test panic")}
	sink := &testAuditSink{}

	// NewDispatcherWithAudit does not yet exist — compile error drives Green Team.
	disp := NewDispatcherWithAudit(snap, map[string]Adapter{"panic.test": adapter}, sink)

	_, _ = disp.Dispatch(context.Background(), panicTestInvocation())

	if len(sink.entries) == 0 {
		t.Fatal("expected at least one audit entry after adapter panic; got none")
	}

	// Find the panic entry.
	var panicEntry map[string]any
	for _, e := range sink.entries {
		if v, ok := e["panic"]; ok {
			if b, ok := v.(bool); ok && b {
				panicEntry = e
				break
			}
		}
	}
	if panicEntry == nil {
		t.Fatal("no audit entry has panic=true (spec line 235 requires panic:true in audit entry)")
	}

	// Required keys per spec line 235.
	requiredKeys := []string{"risk_class", "op_id", "variant_id", "args_hash"}
	for _, k := range requiredKeys {
		if _, ok := panicEntry[k]; !ok {
			t.Errorf("audit entry missing required key %q (spec line 235)", k)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 5: JSON-marshalled response does not leak stack text
// ---------------------------------------------------------------------------

// TestDispatchAdapterPanicResponseDoesNotLeakStackText marshals the returned
// *StructuredError via MarshalJSON and asserts that the bytes contain
// "SERVICE_DOWN" but do not contain any stack-frame markers.
//
// Spec: line 235 — panic stack "NOT in MCP response".
func TestDispatchAdapterPanicResponseDoesNotLeakStackText(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Dispatch propagated panic: %v", r)
		}
	}()

	snap := panicTestCatalog()
	adapter := &panickingAdapter{panicValue: "json_leak_test panic"}
	disp := NewDispatcher(snap, map[string]Adapter{"panic.test": adapter})

	_, err := disp.Dispatch(context.Background(), panicTestInvocation())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("expected *StructuredError, got %T", err)
	}

	b, marshalErr := json.Marshal(se)
	if marshalErr != nil {
		t.Fatalf("MarshalJSON failed: %v", marshalErr)
	}
	jsonStr := string(b)

	if !strings.Contains(jsonStr, "SERVICE_DOWN") {
		t.Errorf("expected JSON to contain 'SERVICE_DOWN', got: %s", jsonStr)
	}

	// Forbidden stack-frame markers (spec: panic stack NOT in MCP response).
	forbidden := []string{"runtime.goexit", "panic:", ".go:", "0x"}
	for _, f := range forbidden {
		if strings.Contains(jsonStr, f) {
			t.Errorf("JSON response must not contain %q (stack leakage); got: %s", f, jsonStr)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 6: table-driven — various panic value types all yield SERVICE_DOWN
// ---------------------------------------------------------------------------

// TestDispatchAdapterPanicWithDifferentPanicValues is table-driven and verifies
// that panics with heterogeneous values (string, int, error, runtime divide-by-zero)
// all produce a SERVICE_DOWN StructuredError and no escaping panic.
//
// Spec: line 235 — "wraps every call to Executor.Execute in a deferred recover()"
// regardless of the type of the panic value.
func TestDispatchAdapterPanicWithDifferentPanicValues(t *testing.T) {
	cases := []struct {
		name       string
		panicValue any
	}{
		{name: "string_panic", panicValue: "string panic"},
		{name: "int_panic", panicValue: 42},
		{name: "error_panic", panicValue: errors.New("err panic")},
		{name: "runtime_divide_by_zero", panicValue: divideByZeroPanic()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			escaped := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						escaped = true
					}
				}()

				snap := panicTestCatalog()
				adapter := &panickingAdapter{panicValue: tc.panicValue}
				disp := NewDispatcher(snap, map[string]Adapter{"panic.test": adapter})

				inv := panicTestInvocation()
				inv.RequestID = fmt.Sprintf("table-%s", tc.name)

				_, err := disp.Dispatch(context.Background(), inv)
				if err == nil {
					t.Errorf("[%s] expected error, got nil", tc.name)
					return
				}

				var se *StructuredError
				if !errors.As(err, &se) {
					t.Errorf("[%s] expected *StructuredError, got %T: %v", tc.name, err, err)
					return
				}
				if se.ErrCode != ErrCodeServiceDown {
					t.Errorf("[%s] expected SERVICE_DOWN, got %q", tc.name, se.ErrCode)
				}
			}()

			if escaped {
				t.Errorf("[%s] panic escaped Dispatch to outer recover — process would crash", tc.name)
			}
		})
	}
}

// divideByZeroPanic recovers the runtime.Error from integer division by zero
// so it can be stored as a value and re-panicked inside panickingAdapter.Execute.
// This simulates a real runtime.Error panic type (e.g. nil pointer dereference).
func divideByZeroPanic() (rv any) {
	defer func() {
		rv = recover()
	}()
	// Force a runtime divide-by-zero (produces runtime.Error).
	var x, y int
	_ = x / y
	return nil
}
