package grpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	stdgrpc "google.golang.org/grpc"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestRegisterLazyInitOnNilInvokerMap pins the
// `a.invokers == nil → a.invokers = map[...]{}` lazy-init arm.
// NewAdapter already populates the map, so the arm is only reachable
// when callers construct &Adapter{} directly (the dispatcher wiring
// MAY do this when zero-value-init from a struct literal). Without
// the lazy init the first Register would panic on a nil map write —
// a crash an operator can't recover from cleanly.
func TestRegisterLazyInitOnNilInvokerMap(t *testing.T) {
	a := &Adapter{}
	if a.invokers != nil {
		t.Fatal("precondition: zero-value Adapter.invokers should be nil")
	}
	called := false
	a.Register("grpc.zero-init", func(ctx context.Context, conn *stdgrpc.ClientConn, args map[string]any) (any, error) {
		called = true
		return nil, nil
	})
	if a.invokers == nil {
		t.Fatal("invokers still nil after Register; lazy init failed")
	}
	fn, ok := a.invokers["grpc.zero-init"]
	if !ok {
		t.Fatal("invokers missing 'grpc.zero-init' entry after Register")
	}
	// Quick sanity invoke to prove the registered fn is intact.
	if _, err := fn(context.Background(), nil, nil); err != nil {
		t.Fatalf("invoker err: %v", err)
	}
	if !called {
		t.Error("registered fn body did not run when invoked")
	}
}

// TestExecuteEmptyAdapterKeySurfacesError pins the
// `bindingKey(rv) == "" → "variant binding has no adapter_key"` arm.
// A misconfigured catalog could ship a variant whose Binding has an
// empty AdapterKey; without this guard Execute would look the empty
// string up in the invokers map and report "no InvokerFunc
// registered for adapter_key """, which is harder to triage.
func TestExecuteEmptyAdapterKeySurfacesError(t *testing.T) {
	a := NewAdapter()
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: ""}},
	}
	_, err := a.Execute(context.Background(), &dispatch.Invocation{}, rv, nil)
	if err == nil {
		t.Fatal("Execute(empty key)=nil err; want adapter_key guard")
	}
	if !strings.Contains(err.Error(), "no adapter_key") {
		t.Errorf("err=%q; want 'no adapter_key' surface", err)
	}
}

// TestExecuteInvokerErrorSurfacesUnwrapped pins the
// `fn err → return nil, err` arm (no extra wrap). The invoker
// returns gRPC status errors directly so the dispatch layer can
// unwrap them to their google.rpc.Status code; an extra "grpc
// adapter:" wrap would mask the status code and break the rpc-code
// mapping in dispatch/handlers.
func TestExecuteInvokerErrorSurfacesUnwrapped(t *testing.T) {
	a := NewAdapter()
	a.Dialer = func(ctx context.Context, rv *dispatch.ResolvedVariant) (*stdgrpc.ClientConn, error) {
		// Return a real ClientConn placeholder is hard; instead, return nil
		// and let the InvokerFunc not dereference it. The Dialer contract
		// only requires "non-nil err signals failure"; nil conn is
		// permissible as long as the invoker tolerates it (this test does).
		return nil, nil
	}
	wantErr := errors.New("rpc unavailable")
	a.Register("grpc.fail", func(ctx context.Context, conn *stdgrpc.ClientConn, args map[string]any) (any, error) {
		return nil, wantErr
	})
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "grpc.fail"}},
	}
	_, err := a.Execute(context.Background(), &dispatch.Invocation{}, rv, nil)
	if !errors.Is(err, wantErr) {
		t.Errorf("err=%v; want unwrapped invoker err %v", err, wantErr)
	}
}

// TestExecuteNilResultReturnsEmptyResponse pins the
// `result == nil → short Response with no body` arm. A gRPC method
// can legitimately return (nil, nil) for empty-success (e.g.
// google.protobuf.Empty unmarshalled to nil); the adapter MUST emit
// a 200 response with empty body rather than calling json.Marshal
// on nil (which would emit the bytes "null" — a semantic difference
// the output pipeline cares about).
func TestExecuteNilResultReturnsEmptyResponse(t *testing.T) {
	a := NewAdapter()
	a.Dialer = func(ctx context.Context, rv *dispatch.ResolvedVariant) (*stdgrpc.ClientConn, error) {
		return nil, nil
	}
	a.Register("grpc.empty", func(ctx context.Context, conn *stdgrpc.ClientConn, args map[string]any) (any, error) {
		return nil, nil
	})
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "grpc.empty"}},
	}
	resp, err := a.Execute(context.Background(), &dispatch.Invocation{}, rv, nil)
	if err != nil {
		t.Fatalf("Execute(nil result): %v", err)
	}
	if resp == nil {
		t.Fatal("resp is nil; want short Response")
	}
	if resp.Body != nil {
		t.Errorf("Body=%q; want nil (no marshal-of-nil)", resp.Body)
	}
	if resp.Format != "json" {
		t.Errorf("Format=%q; want json", resp.Format)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode=%d; want 200", resp.StatusCode)
	}
}

// TestExecuteMarshalErrorSurfacesWrap pins the
// `json.Marshal err → "grpc adapter: marshal response:" wrap` arm.
// A bug in an InvokerFunc could return an unmarshalable Go value
// (e.g. a chan or a func). The wrap MUST include "grpc adapter:"
// so operators can distinguish this from a dispatch-level marshal
// error in the output pipeline. Without the guard the chan would
// trigger json.UnsupportedTypeError downstream where it's harder
// to attribute.
func TestExecuteMarshalErrorSurfacesWrap(t *testing.T) {
	a := NewAdapter()
	a.Dialer = func(ctx context.Context, rv *dispatch.ResolvedVariant) (*stdgrpc.ClientConn, error) {
		return nil, nil
	}
	a.Register("grpc.bad-marshal", func(ctx context.Context, conn *stdgrpc.ClientConn, args map[string]any) (any, error) {
		return map[string]any{"bad": make(chan int)}, nil
	})
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "grpc.bad-marshal"}},
	}
	_, err := a.Execute(context.Background(), &dispatch.Invocation{}, rv, nil)
	if err == nil {
		t.Fatal("Execute(chan)=nil err; want marshal wrap")
	}
	if !strings.Contains(err.Error(), "grpc adapter: marshal response") {
		t.Errorf("err=%q; want 'grpc adapter: marshal response' wrap", err)
	}
}

// TestExecuteDialerErrorSurfacesWrap pins the
// `Dialer err → "grpc adapter: dial:" wrap` arm. The Dialer is the
// only place gRPC connection setup can fail (mTLS handshake, ALTS
// negotiation, etc.). The "grpc adapter: dial:" prefix is the
// operator's grep handle for dial failures in audit logs.
func TestExecuteDialerErrorSurfacesWrap(t *testing.T) {
	a := NewAdapter()
	dialErr := errors.New("tls handshake aborted")
	a.Dialer = func(ctx context.Context, rv *dispatch.ResolvedVariant) (*stdgrpc.ClientConn, error) {
		return nil, dialErr
	}
	a.Register("grpc.unreachable", func(ctx context.Context, conn *stdgrpc.ClientConn, args map[string]any) (any, error) {
		t.Fatal("invoker called despite dialer err")
		return nil, nil
	})
	rv := &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{Binding: &catalog.Binding{AdapterKey: "grpc.unreachable"}},
	}
	_, err := a.Execute(context.Background(), &dispatch.Invocation{}, rv, nil)
	if err == nil {
		t.Fatal("Execute(dial err)=nil err; want dial wrap")
	}
	if !errors.Is(err, dialErr) {
		t.Errorf("err=%v; want unwrap of %v", err, dialErr)
	}
	if !strings.Contains(err.Error(), "grpc adapter: dial") {
		t.Errorf("err=%q; want 'grpc adapter: dial' prefix", err)
	}
}

