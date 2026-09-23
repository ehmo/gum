package dispatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// rawErrorAdapter fails with a plain error, the way every `fmt.Errorf` site in
// internal/adapters does: "typedrestsdk: missing HTTP binding for op X",
// "plugin.mcp: no host configured", and so on.
type rawErrorAdapter struct{ err error }

func (a rawErrorAdapter) Execute(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	return nil, a.err
}

// Spec §7 gives every dispatch failure a parseable envelope with a stable
// error_code. An adapter that fails with a plain error used to travel out of
// Dispatch unwrapped, so the caller got free text with no code, no retryable
// flag, and nothing to branch on. §3.1 step 7 already sets the precedent for
// this class: an executor panic becomes SERVICE_DOWN with retryable=false.
func TestUnstructuredAdapterErrorGetsEnvelope(t *testing.T) {
	const opID = "test.raw.err"
	const adapterKey = "test.adapter.raw.err"
	cause := errors.New("typedrestsdk: missing HTTP binding for op test.raw.err")

	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: rawErrorAdapter{err: cause}},
		dispatch.DispatcherConfig{},
	)

	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:      opID,
		Args:      map[string]any{},
		Format:    "json",
		RequestID: "raw-err-1",
	})
	if err == nil {
		t.Fatal("Dispatch = nil err; want a structured error")
	}

	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("Dispatch err = %T (%v); want it to resolve to *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeServiceDown {
		t.Errorf("error_code = %q; want %q", se.ErrCode, dispatch.ErrCodeServiceDown)
	}
	if se.Retryable {
		t.Error("retryable = true; an adapter contract failure is not worth retrying")
	}

	// The cause must stay reachable so the CLI can log it and errors.Is keeps
	// working for callers that match on a sentinel.
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(err, cause) = false; the original error must stay wrapped (got %v)", err)
	}

	body, merr := json.Marshal(se)
	if merr != nil {
		t.Fatalf("marshal envelope: %v", merr)
	}
	var obj map[string]any
	if uerr := json.Unmarshal(body, &obj); uerr != nil {
		t.Fatalf("envelope is not valid JSON: %v", uerr)
	}
	if obj["error_code"] != "SERVICE_DOWN" {
		t.Errorf("envelope error_code = %v; want SERVICE_DOWN (envelope: %s)", obj["error_code"], body)
	}
	if _, ok := obj["op_id"]; !ok {
		t.Errorf("envelope has no op_id; the caller cannot tell which call failed (envelope: %s)", body)
	}
}

// A structured adapter error must pass through untouched: the wrap is a
// fallback, not a rewrite.
func TestStructuredAdapterErrorPassesThrough(t *testing.T) {
	const opID = "test.structured.err"
	const adapterKey = "test.adapter.structured.err"
	cause := dispatch.NewStructuredError(dispatch.ErrCodeRateLimited, "slow down").WithRetryable(true)

	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: rawErrorAdapter{err: cause}},
		dispatch.DispatcherConfig{},
	)

	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:      opID,
		Args:      map[string]any{},
		Format:    "json",
		RequestID: "structured-err-1",
	})
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("Dispatch err = %T (%v); want *dispatch.StructuredError", err, err)
	}
	if se.ErrCode != dispatch.ErrCodeRateLimited {
		t.Errorf("error_code = %q; want RATE_LIMITED preserved, not rewritten", se.ErrCode)
	}
	if !se.Retryable {
		t.Error("retryable = false; the adapter's own retryable flag must survive")
	}
}
