package adapters_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// mockDispatcher is a test-only dispatch.Dispatcher whose Dispatch method
// forwards to a caller-supplied function.
type mockDispatcher struct {
	fn func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error)
}

func (m *mockDispatcher) Dispatch(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return m.fn(ctx, inv)
}

// minimalCodeVariant returns a ResolvedVariant suitable for gum.code Execute calls.
func minimalCodeVariant() *dispatch.ResolvedVariant {
	return &dispatch.ResolvedVariant{
		OpID:       "gum.code",
		Variant:    &catalog.Variant{VariantID: "gum.code.v1", RiskClass: catalog.RiskClassRead},
		AdapterKey: "code.risor",
	}
}

// execScript runs a Risor script under a CodeRunner wired with mock, returns
// the captured gum_print output as a trimmed string.
func execScript(t *testing.T, ctx context.Context, mock *mockDispatcher, script string) string {
	t.Helper()
	cr := adapters.NewCodeRunner().WithDispatcher(mock)
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{"language": "risor", "source": script},
	}
	resp, err := cr.Execute(ctx, inv, minimalCodeVariant(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return strings.TrimSpace(string(resp.Body))
}

func execInvocation(t *testing.T, ctx context.Context, mock *mockDispatcher, inv *dispatch.Invocation) string {
	t.Helper()
	cr := adapters.NewCodeRunner().WithDispatcher(mock)
	resp, err := cr.Execute(ctx, inv, minimalCodeVariant(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return strings.TrimSpace(string(resp.Body))
}

// buildEntryList returns a Risor expression that builds a length-n list of
// {op: opID} maps using list literal syntax (Risor has no for-loops).
func buildEntryList(n int, opID string) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(`{op: "`)
		b.WriteString(opID)
		b.WriteString(`"}`)
	}
	b.WriteString("]")
	return b.String()
}

func TestGumCallDispatchesReadAndReturnsResult(t *testing.T) {
	var calls atomic.Int32
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		calls.Add(1)
		if inv.OpID != "op.read" {
			t.Errorf("OpID = %q; want op.read", inv.OpID)
		}
		if inv.AllowWrite || inv.AllowDestructive || inv.Confirmed || inv.ConfirmationToken != "" {
			t.Errorf("read probe carried elevated flags: %#v", inv)
		}
		if got := inv.Args["x"]; got != int64(1) && got != 1 && got != float64(1) {
			t.Errorf("Args[x] = %#v; want 1", got)
		}
		return &dispatch.ShapedResponse{
			Format:            "json",
			StructuredContent: map[string]any{"marker": "from_dispatch"},
		}, nil
	}}

	out := execScript(t, context.Background(), mock, `gum_print(gum_call("op.read", {"x": 1})["marker"])`)
	if out != "from_dispatch" {
		t.Fatalf("gum_call output = %q; want dispatcher result", out)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("Dispatch calls = %d; want 1", got)
	}
}

func TestGumCallWriteRetriesOnlyWhenAllowWrite(t *testing.T) {
	var calls atomic.Int32
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		call := calls.Add(1)
		switch call {
		case 1:
			if inv.AllowWrite {
				t.Fatal("first write probe must not carry AllowWrite")
			}
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeRiskToolMismatch, "write requires gum.write").
				WithDetail("required_tool", "gum.write")
		case 2:
			if !inv.AllowWrite || inv.AllowDestructive {
				t.Fatalf("second write dispatch flags = AllowWrite:%t AllowDestructive:%t", inv.AllowWrite, inv.AllowDestructive)
			}
			return &dispatch.ShapedResponse{StructuredContent: map[string]any{"ok": "write"}}, nil
		default:
			t.Fatalf("unexpected dispatch call %d", call)
			return nil, nil
		}
	}}
	inv := &dispatch.Invocation{
		OpID:       "gum.code",
		Args:       map[string]any{"language": "risor", "source": `gum_print(gum_call("op.write", {})["ok"])`},
		AllowWrite: true,
		Confirmed:  true,
	}
	out := execInvocation(t, context.Background(), mock, inv)
	if out != "write" {
		t.Fatalf("gum_call output = %q; want write", out)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("Dispatch calls = %d; want 2", got)
	}
}

func TestGumCallWriteDeniedWithoutAllowWrite(t *testing.T) {
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeRiskToolMismatch, "write requires gum.write").
			WithDetail("required_tool", "gum.write")
	}}
	cr := adapters.NewCodeRunner().WithDispatcher(mock)
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{"language": "risor", "source": `gum_call("op.write", {})`},
	}
	_, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if !dispatch.IsStructuredError(err, dispatch.ErrCodeRiskToolMismatch) {
		t.Fatalf("Execute err = %v; want RISK_TOOL_MISMATCH", err)
	}
}

func TestGumCallWriteDeniedWithoutOuterConfirmation(t *testing.T) {
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeRiskToolMismatch, "write requires gum.write").
			WithDetail("required_tool", "gum.write")
	}}
	cr := adapters.NewCodeRunner().WithDispatcher(mock)
	inv := &dispatch.Invocation{
		OpID:       "gum.code",
		Args:       map[string]any{"language": "risor", "source": `gum_call("op.write", {})`},
		AllowWrite: true,
	}
	_, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if !dispatch.IsStructuredError(err, dispatch.ErrCodeRequiresConfirmation) {
		t.Fatalf("Execute err = %v; want REQUIRES_CONFIRMATION", err)
	}
}

func TestGumCallDestructiveUsesLocalGateThenKernelToken(t *testing.T) {
	var calls atomic.Int32
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		call := calls.Add(1)
		switch call {
		case 1:
			if inv.AllowDestructive || inv.Confirmed {
				t.Fatalf("first destructive probe carried elevated flags: %#v", inv)
			}
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeRiskToolMismatch, "destructive requires gum.destructive").
				WithDetail("required_tool", "gum.destructive")
		case 2:
			if !inv.AllowDestructive || inv.Confirmed || inv.ConfirmationToken != "" {
				t.Fatalf("first destructive token request flags wrong: %#v", inv)
			}
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeRequiresConfirmation, "confirm").
				WithDetail("confirmation_token", "tok-test")
		case 3:
			if !inv.AllowDestructive || !inv.Confirmed || inv.ConfirmationToken != "tok-test" {
				t.Fatalf("confirmed destructive dispatch flags wrong: %#v", inv)
			}
			return &dispatch.ShapedResponse{StructuredContent: map[string]any{"deleted": "yes"}}, nil
		default:
			t.Fatalf("unexpected dispatch call %d", call)
			return nil, nil
		}
	}}
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{
			"language":           "risor",
			"source":             `gum_confirm_destructive("op.delete", "r1"); gum_print(gum_call("op.delete", {"id": "r1"})["deleted"])`,
			"destructive_budget": 1,
			"destructive_scope": []any{
				map[string]any{"op_id": "op.delete", "resource_key": "r1"},
			},
		},
		AllowDestructive: true,
		Confirmed:        true,
	}
	out := execInvocation(t, context.Background(), mock, inv)
	if out != "yes" {
		t.Fatalf("gum_call output = %q; want yes", out)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("Dispatch calls = %d; want 3", got)
	}
}

// TestGumParallelBoundedFanOut asserts that with 20 input elements and a
// per-call sleep, max concurrent workers never exceeds the spec §6.3 bound of
// 8 and all 20 elements are dispatched.
func TestGumParallelBoundedFanOut(t *testing.T) {
	const n = 20
	var inFlight, maxConcurrent atomic.Int32
	var completed atomic.Int32

	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		cur := inFlight.Add(1)
		for {
			m := maxConcurrent.Load()
			if cur <= m || maxConcurrent.CompareAndSwap(m, cur) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		inFlight.Add(-1)
		completed.Add(1)
		return &dispatch.ShapedResponse{
			Body:              []byte(`{"ok":true}`),
			Format:            "json",
			StructuredContent: map[string]any{"ok": true},
		}, nil
	}}

	script := `gum_parallel(` + buildEntryList(n, "op.test") + `)`
	execScript(t, context.Background(), mock, script)

	if got := completed.Load(); got != int32(n) {
		t.Errorf("completed = %d; want %d", got, n)
	}
	if got := maxConcurrent.Load(); got > 8 {
		t.Errorf("maxConcurrent = %d; spec §6.3 caps at 8", got)
	}
	if got := maxConcurrent.Load(); got < 2 {
		t.Errorf("maxConcurrent = %d; expected real concurrency >= 2 with n=20 and 15ms sleep", got)
	}
}

// TestGumParallelPreservesInputOrder asserts that results are returned in
// input order regardless of completion order. Uses staggered sleeps so the
// later-input elements finish first.
func TestGumParallelPreservesInputOrder(t *testing.T) {
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		switch inv.OpID {
		case "op.a":
			time.Sleep(30 * time.Millisecond)
		case "op.b":
			time.Sleep(15 * time.Millisecond)
		}
		return &dispatch.ShapedResponse{
			StructuredContent: map[string]any{"who": inv.OpID},
			Format:            "json",
		}, nil
	}}

	script := `
let env = gum_parallel([{op: "op.a"}, {op: "op.b"}, {op: "op.c"}])
env["results"].each(r => gum_print(r["_expression"]["op_id"] + ","))
`
	got := execScript(t, context.Background(), mock, script)
	want := "op.a,op.b,op.c,"
	if got != want {
		t.Errorf("input order not preserved: got %q, want %q", got, want)
	}
}

// TestGumParallelCancellationProducesCANCELLED is implemented as a white-box
// test in code_risor_parallel_internal_test.go (same package), because the
// Risor VM aborts on context cancellation before the script can inspect the
// returned envelope. White-box bypasses Risor and exercises the worker pool
// directly.

// TestGumParallelEnvelopeShape asserts the §9.0.1 outer envelope contract:
// format="parallel_results", batch_id present, outer _expression with
// op_id="gum_parallel" and variant_id=null, and results carry _idx+_expression.
func TestGumParallelEnvelopeShape(t *testing.T) {
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{
			Body:              []byte(`{"ok":true}`),
			Format:            "json",
			StructuredContent: map[string]any{"ok": true},
		}, nil
	}}
	script := `
let env = gum_parallel([{op: "op.a"}, {op: "op.b"}])
gum_print(env["format"] + "|")
gum_print(env["batch_id"])
gum_print("|" + env["_expression"]["op_id"] + "|")
let v = env["_expression"].get("variant_id")
if (v == nil) { gum_print("nil") } else { gum_print(v) }
gum_print("|")
gum_print(env["results"][0]["_idx"])
gum_print(",")
gum_print(env["results"][1]["_idx"])
gum_print("|")
gum_print(env["results"][0]["_expression"]["op_id"])
gum_print(",")
gum_print(env["results"][1]["_expression"]["op_id"])
`
	got := execScript(t, context.Background(), mock, script)
	// batch_id is 8 hex chars; replace the actual value with a placeholder.
	parts := strings.SplitN(got, "|", 3)
	if len(parts) < 3 {
		t.Fatalf("output missing | separators: %q", got)
	}
	if len(parts[1]) != 8 {
		t.Errorf("batch_id length = %d, want 8: %q", len(parts[1]), parts[1])
	}
	normalised := parts[0] + "|<batch_id>|" + parts[2]
	want := "parallel_results|<batch_id>|gum_parallel|nil|0,1|op.a,op.b"
	if normalised != want {
		t.Errorf("envelope shape mismatch:\ngot:  %q\nwant: %q", normalised, want)
	}
}

// TestGumParallelPerElementErrorXOR asserts that each result carries either
// a payload (`format`+`data`) OR an `error` envelope, never both (spec §9.0.1).
func TestGumParallelPerElementErrorXOR(t *testing.T) {
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		if inv.OpID == "op.fail" {
			return nil, dispatch.NewStructuredError(dispatch.ErrCodeServiceDown, "fake outage").
				WithDetail("op_id", inv.OpID)
		}
		return &dispatch.ShapedResponse{
			Format:            "json",
			StructuredContent: map[string]any{"ok": true},
		}, nil
	}}
	script := `
let env = gum_parallel([{op: "op.ok"}, {op: "op.fail"}, {op: "op.ok"}])
let out = ""
env["results"].each(r => {
    let e = r.get("error")
    if (e != nil) {
        out = out + "E:" + e["error_code"] + ";"
    } else {
        out = out + "OK:" + r["format"] + ";"
    }
})
gum_print(out)
`
	got := execScript(t, context.Background(), mock, script)
	want := "OK:json;E:SERVICE_DOWN;OK:json;"
	if got != want {
		t.Errorf("per-element XOR mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestGumParallelSharedExpressionFieldsHoist asserts the §9.0.1 hoist rule:
// fields present and identical across all N results are hoisted into
// shared_expression_fields and removed from per-result _expression.
func TestGumParallelSharedExpressionFieldsHoist(t *testing.T) {
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{Format: "json", StructuredContent: map[string]any{"ok": true}}, nil
	}}
	script := `
let env = gum_parallel([{op: "op.same"}, {op: "op.same"}, {op: "op.same"}])
let shared = env.get("shared_expression_fields")
let out = ""
if (shared != nil) { out = out + "shared.op_id=" + shared["op_id"] + ";" } else { out = out + "shared=nil;" }
env["results"].each(r => {
    let pid = r["_expression"].get("op_id")
    if (pid != nil) {
        out = out + "P:" + pid + ";"
    } else {
        out = out + "P:hoisted;"
    }
})
gum_print(out)
`
	got := execScript(t, context.Background(), mock, script)
	want := "shared.op_id=op.same;P:hoisted;P:hoisted;P:hoisted;"
	if got != want {
		t.Errorf("hoist mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestGumParallelNoHoistWhenDiffers asserts the hoist negative case: when
// fields differ across results, no field is hoisted; per-result _expression
// keeps each value.
func TestGumParallelNoHoistWhenDiffers(t *testing.T) {
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{Format: "json", StructuredContent: map[string]any{"ok": true}}, nil
	}}
	script := `
let env = gum_parallel([{op: "op.a"}, {op: "op.b"}])
let shared = env.get("shared_expression_fields")
let out = ""
if (shared == nil) { out = out + "shared=nil;" } else { out = out + "shared.op_id=" + shared["op_id"] + ";" }
out = out + "a=" + env["results"][0]["_expression"]["op_id"] + ";"
out = out + "b=" + env["results"][1]["_expression"]["op_id"] + ";"
gum_print(out)
`
	got := execScript(t, context.Background(), mock, script)
	want := "shared=nil;a=op.a;b=op.b;"
	if got != want {
		t.Errorf("no-hoist mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

// TestGumParallelMissingOpIDRejected asserts that pre-flight input validation
// rejects malformed element lists (spec §6.3 lines 1004-1005: "raises only
// when pre-flight validation rejects the whole batch before dispatch starts").
func TestGumParallelMissingOpIDRejected(t *testing.T) {
	mock := &mockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		t.Errorf("dispatcher should NOT be invoked when pre-flight validation fails")
		return nil, nil
	}}
	cr := adapters.NewCodeRunner().WithDispatcher(mock)
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{
			"language": "risor",
			"source":   `gum_parallel([{args: {}}])`, // missing op_id
		},
	}
	_, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err == nil {
		t.Fatal("expected error for missing op_id, got nil")
	}
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("expected StructuredError, got %T: %v", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeInvalidArgs {
		t.Errorf("expected INVALID_ARGS, got %s", se.ErrCode)
	}
}

// TestGumParallelDispatcherNotWiredErrors asserts that when CodeRunner has no
// dispatcher, gum_parallel raises INVALID_ARGS (not a panic).
func TestGumParallelDispatcherNotWiredErrors(t *testing.T) {
	cr := adapters.NewCodeRunner() // no WithDispatcher
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{
			"language": "risor",
			"source":   `gum_parallel([{op: "op.a"}])`,
		},
	}
	_, err := cr.Execute(context.Background(), inv, minimalCodeVariant(), nil)
	if err == nil {
		t.Fatal("expected error when dispatcher unwired, got nil")
	}
	if !strings.Contains(err.Error(), "not wired") && !strings.Contains(err.Error(), "INVALID_ARGS") {
		t.Errorf("expected INVALID_ARGS / 'not wired', got: %v", err)
	}
}
