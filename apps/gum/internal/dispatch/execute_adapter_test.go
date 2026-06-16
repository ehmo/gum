package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestExecuteAdapterPassesContext verifies the adapter receives the exact
// context passed to executeAdapter (so cancellation, deadline, and request-
// scoped values propagate).
// Acceptance: adapter receives the same ctx passed to Dispatch.
func TestExecuteAdapterPassesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type ctxKey string
	const k ctxKey = "rid"
	ctx = context.WithValue(ctx, k, "req-42")

	a := &captureAdapter{want: ctx, key: k}
	d := &dispatcher{adapters: map[string]Adapter{"test": a}}

	_, err := d.executeAdapter(ctx, &Invocation{OpID: "x"}, &ResolvedVariant{AdapterKey: "test", Variant: &catalog.Variant{}}, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !a.sawValue {
		t.Error("adapter did not receive request-scoped context value (ctx was copied / replaced)")
	}
}

// TestExecuteAdapterCancellationReturnsStructured verifies a cancelled context
// during adapter Execute is surfaced as a structured CANCELLED error (spec
// §1421 stable runtime error codes).
// Acceptance: ctx cancel mid-execute causes Execute to return CANCELLED.
func TestExecuteAdapterCancellationReturnsStructured(t *testing.T) {
	slow := &slowAdapter{block: 50 * time.Millisecond}
	d := &dispatcher{adapters: map[string]Adapter{"slow": slow}}

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err := d.executeAdapter(ctx, &Invocation{OpID: "x"}, &ResolvedVariant{AdapterKey: "slow", Variant: &catalog.Variant{}}, nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !IsStructuredError(err, ErrCodeCancelled) {
		t.Fatalf("got %v, want CANCELLED", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err must satisfy errors.Is(context.Canceled); chain: %v", err)
	}
}

// TestExecuteAdapterAdapterNotFoundCarriesKey verifies an unknown adapter_key
// produces a structured SERVICE_DOWN error carrying the adapter key in the
// detail field so audit log + user diagnostics name the missing executor.
// Acceptance: ADAPTER_NOT_FOUND error carries adapter key.
func TestExecuteAdapterAdapterNotFoundCarriesKey(t *testing.T) {
	d := &dispatcher{adapters: map[string]Adapter{}}
	_, err := d.executeAdapter(t.Context(), &Invocation{OpID: "x"}, &ResolvedVariant{AdapterKey: "code.risor", Variant: &catalog.Variant{}}, nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !IsStructuredError(err, ErrCodeServiceDown) {
		t.Fatalf("err code: got %v, want SERVICE_DOWN", err)
	}
	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatal("err is not *StructuredError")
	}
	if got := se.Detail["adapter_key"]; got != "code.risor" {
		t.Errorf("Detail[adapter_key]=%v, want code.risor", got)
	}
}

// TestExecuteAdapterPanicConvertsToServiceDown is a guard test ensuring the
// existing recoverAdapterPanic deferred catch still maps panic → SERVICE_DOWN.
// (Pre-existing behavior; this test exists so step-7 hardening cannot break it.)
func TestExecuteAdapterPanicConvertsToServiceDown(t *testing.T) {
	a := &panicAdapter{}
	d := &dispatcher{adapters: map[string]Adapter{"panic": a}}
	_, err := d.executeAdapter(t.Context(), &Invocation{OpID: "x"}, &ResolvedVariant{AdapterKey: "panic", Variant: &catalog.Variant{}}, nil)
	if err == nil {
		t.Fatal("want error from panicking adapter, got nil")
	}
	if !IsStructuredError(err, ErrCodeServiceDown) {
		t.Fatalf("got %v, want SERVICE_DOWN", err)
	}
}

type captureAdapter struct {
	want     context.Context
	key      any
	sawValue bool
}

func (c *captureAdapter) Execute(ctx context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
	if ctx.Value(c.key) == c.want.Value(c.key) && c.want.Value(c.key) != nil {
		c.sawValue = true
	}
	return &Response{Body: []byte(`{}`)}, nil
}

type slowAdapter struct {
	block time.Duration
}

func (s *slowAdapter) Execute(ctx context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
	select {
	case <-time.After(s.block):
		return &Response{Body: []byte(`{}`)}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type panicAdapter struct{}

func (p *panicAdapter) Execute(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
	panic("adapter exploded")
}
