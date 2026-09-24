package adapters_test

// Spec §6.1 "Code-output budget enforcement" (docs/test-matrix.md).
//
// gum.code carries ONE cumulative byte budget over every gum_print plus the
// JSON projection of the script's return value. Default 4096 bytes. A print
// that would cross the line is cut at a UTF-8 boundary and the envelope says
// so; a return value that would cross it replaces the whole result with
// CODE_OUTPUT_LIMIT_EXCEEDED.

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// specOutputLimitBytes is the §6.1 default the matrix row pins.
const specOutputLimitBytes = 4096

// specMaxPrintBytesPerCall is the §6.3 per-call gum_print ceiling. It binds
// only when the cumulative budget is raised above it.
const specMaxPrintBytesPerCall = 4096

// specParallelCeilingBytes is the §9.0.1 pre-dispatch aggregate ceiling at the
// shipped defaults: 4096 bytes x 8 workers.
const specParallelCeilingBytes = 32768

// runBudgetCode executes source as a read-only gum.code invocation. limit 0
// leaves the shipped default in force.
func runBudgetCode(t *testing.T, limit int, source string) (*dispatch.Response, error) {
	t.Helper()

	cr := adapters.NewCodeRunner().WithOutputLimitBytes(limit)
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{"language": "risor", "source": source},
	}
	return cr.Execute(context.Background(), inv, makeMinimalVariant(), nil)
}

// runBudgetBatch executes source with a dispatcher wired, so gum_parallel is
// live and the §9.0.1 ceilings apply. It reports the response, how many
// elements reached the kernel, and the error. The counter is atomic because
// gum_parallel calls the kernel from up to 8 worker goroutines at once.
func runBudgetBatch(t *testing.T, limit int, payloadBytes int, source string) (*dispatch.Response, int, error) {
	t.Helper()

	var dispatched atomic.Int64
	body := strings.Repeat("x", payloadBytes)
	mock := &mockDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		dispatched.Add(1)
		return &dispatch.ShapedResponse{Format: "toon", Body: []byte(body)}, nil
	}}

	cr := adapters.NewCodeRunner().WithDispatcher(mock).WithOutputLimitBytes(limit)
	inv := &dispatch.Invocation{
		OpID: "gum.code",
		Args: map[string]any{"language": "risor", "source": source},
	}
	resp, err := cr.Execute(context.Background(), inv, makeMinimalVariant(), nil)
	return resp, int(dispatched.Load()), err
}

// oversizedBatchSource is a one-element batch whose declared argument alone
// overruns the 32768-byte default aggregate ceiling.
func oversizedBatchSource() string {
	return `gum_parallel([{op: "op.x", args: {q: "` + strings.Repeat("a", 40000) + `"}}])`
}

// printSource builds a script that prints n copies of one ASCII byte.
func printSource(n int) string {
	return `gum_print("` + strings.Repeat("x", n) + `")`
}

// requireLimitError asserts err is the §6.1 terminal envelope and returns it.
func requireLimitError(t *testing.T, err error) *dispatch.StructuredError {
	t.Helper()

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v (%T); want a *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeCodeOutputLimitExceeded {
		t.Fatalf("error_code = %q; want %q", se.ErrCode, dispatch.ErrCodeCodeOutputLimitExceeded)
	}
	return se
}

func TestCodeOutputBudget(t *testing.T) {
	t.Run("prints share one cumulative budget", func(t *testing.T) {
		// Two prints of 3000 bytes each. Neither trips the per-call ceiling;
		// together they overrun the 4096-byte cumulative budget, so the second
		// one is cut to what the first left.
		source := printSource(3000) + "\n" + printSource(3000)

		resp, err := runBudgetCode(t, 0, source)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(resp.Body) != specOutputLimitBytes {
			t.Errorf("printed %d bytes; want exactly the %d-byte budget", len(resp.Body), specOutputLimitBytes)
		}
		if !resp.CodeOutputTruncated {
			t.Error("CodeOutputTruncated = false; a print lost bytes, so the envelope must say so")
		}
	})

	t.Run("an untruncated script never sets the flag", func(t *testing.T) {
		resp, err := runBudgetCode(t, 0, printSource(10))
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if resp.CodeOutputTruncated {
			t.Error("CodeOutputTruncated = true for a 10-byte print inside a 4096-byte budget")
		}
	})

	t.Run("truncation lands on a UTF-8 boundary", func(t *testing.T) {
		// "é" is 2 bytes. An odd budget cannot be met on a rune boundary, so
		// the cut must walk back one byte rather than split the rune.
		const limit = 3001
		source := `gum_print("` + strings.Repeat("é", 4000) + `")`

		resp, err := runBudgetCode(t, limit, source)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(resp.Body) != limit-1 {
			t.Errorf("printed %d bytes; want %d, the last 2-byte boundary below the %d-byte budget", len(resp.Body), limit-1, limit)
		}
		if !utf8.Valid(resp.Body) {
			t.Error("printed bytes are not valid UTF-8 after truncation")
		}
	})

	t.Run("an oversized return value replaces the result", func(t *testing.T) {
		// The whole script is one string literal, so it is also the return
		// value: 5000 bytes against a 4096-byte budget with nothing printed.
		source := `"` + strings.Repeat("x", 5000) + `"`

		resp, err := runBudgetCode(t, 0, source)
		if err == nil {
			t.Fatalf("Execute returned a response (%d body bytes) and no error; the return value overruns the budget", len(resp.Body))
		}
		se := requireLimitError(t, err)
		if got := se.Detail["limit_bytes"]; got != specOutputLimitBytes {
			t.Errorf("limit_bytes = %v; want %d", got, specOutputLimitBytes)
		}
		if got := se.Detail["printed_bytes"]; got != 0 {
			t.Errorf("printed_bytes = %v; want 0, nothing was printed", got)
		}
	})

	t.Run("the return value shares the budget with prints", func(t *testing.T) {
		// 3000 printed bytes leave 1096. A 2000-byte return value does not fit
		// even though neither half exceeds the budget alone.
		source := printSource(3000) + "\n" + `"` + strings.Repeat("y", 2000) + `"`

		_, err := runBudgetCode(t, 0, source)
		if err == nil {
			t.Fatal("Execute succeeded; 3000 printed bytes plus a 2000-byte return value overrun the 4096-byte budget")
		}
		se := requireLimitError(t, err)
		if got := se.Detail["printed_bytes"]; got != 3000 {
			t.Errorf("printed_bytes = %v; want 3000", got)
		}
	})

	t.Run("a return value that fits is allowed through", func(t *testing.T) {
		source := printSource(100) + "\n" + `"` + strings.Repeat("y", 100) + `"`

		if _, err := runBudgetCode(t, 0, source); err != nil {
			t.Fatalf("Execute: %v; 200 bytes fit inside the 4096-byte budget", err)
		}
	})

	t.Run("the configured limit replaces the default", func(t *testing.T) {
		const limit = 64

		resp, err := runBudgetCode(t, limit, printSource(500))
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(resp.Body) != limit {
			t.Errorf("printed %d bytes under code.output_limit_bytes=%d", len(resp.Body), limit)
		}
		if !resp.CodeOutputTruncated {
			t.Error("CodeOutputTruncated = false after a 500-byte print into a 64-byte budget")
		}
	})

	t.Run("a parallel batch truncates against the script budget", func(t *testing.T) {
		// Three 2000-byte elements against the 4096-byte default: the batch
		// cannot fit, so §9.0.1 cuts the later elements and flags them.
		source := `let env = gum_parallel([{op: "op.x"}, {op: "op.x"}, {op: "op.x"}])
gum_print(env["_code_output_truncated"])
gum_print(":")
gum_print(env["results"][2]["_code_output_truncated"])`

		resp, dispatched, err := runBudgetBatch(t, 0, 2000, source)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if dispatched != 3 {
			t.Fatalf("dispatched %d elements; want 3", dispatched)
		}
		if got := string(resp.Body); got != "true:true" {
			t.Errorf("printed %q; want \"true:true\" — the outer and per-element §9.0.1 flags", got)
		}
	})

	t.Run("an untruncated parallel batch leaves both flags off", func(t *testing.T) {
		// Three 20-byte elements fit inside the 4096-byte budget, so neither
		// flag may appear.
		source := `let env = gum_parallel([{op: "op.x"}, {op: "op.x"}, {op: "op.x"}])
gum_print(env.get("_code_output_truncated", "absent"))
gum_print(":")
gum_print(env["results"][2].get("_code_output_truncated", "absent"))`

		resp, _, err := runBudgetBatch(t, 0, 20, source)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if got := string(resp.Body); got != "absent:absent" {
			t.Errorf("printed %q; want \"absent:absent\"", got)
		}
	})

	t.Run("an oversized batch is refused before dispatch", func(t *testing.T) {
		_, dispatched, err := runBudgetBatch(t, 0, 20, oversizedBatchSource())
		if err == nil {
			t.Fatal("Execute succeeded; a 40000-byte batch input overruns the 32768-byte aggregate ceiling")
		}
		if dispatched != 0 {
			t.Fatalf("dispatched %d elements; the §9.0.1 ceiling must refuse before the first dispatch", dispatched)
		}
		se := requireLimitError(t, err)
		if got := se.Detail["limit_bytes"]; got != specParallelCeilingBytes {
			t.Errorf("limit_bytes = %v; want %d", got, specParallelCeilingBytes)
		}
		requested, ok := se.Detail["requested_bytes"].(int)
		if !ok || requested < 40000 {
			t.Errorf("requested_bytes = %v; want the declared input size, at least 40000", se.Detail["requested_bytes"])
		}
	})

	t.Run("a raised output limit raises the batch ceiling", func(t *testing.T) {
		// The same batch the default refuses is accepted once
		// code.output_limit_bytes doubles: the ceiling tracks it x 8 workers.
		_, dispatched, err := runBudgetBatch(t, 8192, 20, oversizedBatchSource())
		if err != nil {
			t.Fatalf("Execute: %v; a 65536-byte ceiling covers a 40000-byte batch input", err)
		}
		if dispatched != 1 {
			t.Fatalf("dispatched %d elements; want 1", dispatched)
		}
	})

	t.Run("one print cannot exceed the per-call ceiling", func(t *testing.T) {
		// A raised cumulative budget still leaves the per-call ceiling in
		// place, so a single huge print is cut to 4096 bytes.
		resp, err := runBudgetCode(t, 20000, printSource(10000))
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if len(resp.Body) != specMaxPrintBytesPerCall {
			t.Errorf("printed %d bytes in one call; the per-call ceiling is %d", len(resp.Body), specMaxPrintBytesPerCall)
		}
		if !resp.CodeOutputTruncated {
			t.Error("CodeOutputTruncated = false after the per-call ceiling cut the print")
		}
	})
}
