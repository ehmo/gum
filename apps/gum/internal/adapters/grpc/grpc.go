// Package grpc is the backend executor for catalog variants with
// backend_kind="grpc-sdk" (spec §5.7, §14). It wraps gRPC clients from
// the cloud.google.com/go family (Spanner, Pub/Sub, BigQuery, Bigtable,
// …) without locking the dispatcher into any specific service's import
// set: variants register a per-op InvokerFunc that knows how to translate
// dispatch.Invocation.Args into a gRPC call on a typed client.
//
// The decoupling matters because cloud.google.com/go clients are heavy
// transitive dependencies. Pulling Spanner alone adds ~50 modules; not
// every gum build needs every Google gRPC client. Keeping the adapter
// layer agnostic lets the runtime wire only the clients a given binary
// needs — the catalog tells us which adapter_keys to register and
// nothing more.
package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"google.golang.org/grpc"

	"github.com/ehmo/gum/internal/dispatch"
)

// InvokerFunc executes a single gRPC method against the supplied
// connection using args from the dispatch.Invocation. The return value
// is the typed response — the adapter marshals it to JSON for the
// downstream output pipeline. Returning (nil, nil) is treated as an
// empty body.
type InvokerFunc func(ctx context.Context, conn *grpc.ClientConn, args map[string]any) (any, error)

// Adapter routes catalog variants with backend_kind="grpc-sdk" to
// per-adapter_key InvokerFuncs. The Dialer constructs (or returns a
// cached) grpc.ClientConn for a given variant; tests inject a bufconn
// dialer so the contract is verifiable offline.
type Adapter struct {
	mu       sync.RWMutex
	invokers map[string]InvokerFunc

	// Dialer returns the grpc.ClientConn to use for the given variant.
	// The dispatcher caches connections internally — this function may
	// memoize as appropriate. Required; the adapter has no sensible
	// default because Google's mutual-TLS settings differ per service.
	Dialer func(ctx context.Context, rv *dispatch.ResolvedVariant) (*grpc.ClientConn, error)
}

// NewAdapter returns an empty Adapter; callers register InvokerFuncs
// via Register and set Dialer before passing to the dispatcher.
func NewAdapter() *Adapter {
	return &Adapter{invokers: map[string]InvokerFunc{}}
}

// Register associates an InvokerFunc with a binding adapter_key. Calling
// Register a second time for the same key overrides the previous entry —
// this is the catalog-extension hook, so post-init mutation is rare and
// always under operator control.
func (a *Adapter) Register(adapterKey string, fn InvokerFunc) {
	a.mu.Lock()
	if a.invokers == nil {
		a.invokers = map[string]InvokerFunc{}
	}
	a.invokers[adapterKey] = fn
	a.mu.Unlock()
}

// Execute is the dispatch.Adapter entry point. It looks up the
// adapter_key, dials the gRPC connection via Dialer, calls the
// registered InvokerFunc, then marshals the response as JSON for the
// output pipeline.
func (a *Adapter) Execute(ctx context.Context, inv *dispatch.Invocation, rv *dispatch.ResolvedVariant, creds *dispatch.Credentials) (*dispatch.Response, error) {
	key := bindingKey(rv)
	if key == "" {
		return nil, errors.New("grpc adapter: variant binding has no adapter_key")
	}
	a.mu.RLock()
	fn, ok := a.invokers[key]
	a.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("grpc adapter: no InvokerFunc registered for adapter_key %q", key)
	}
	if a.Dialer == nil {
		return nil, errors.New("grpc adapter: Dialer is nil; the dispatcher wiring must supply one")
	}
	conn, err := a.Dialer(ctx, rv)
	if err != nil {
		return nil, fmt.Errorf("grpc adapter: dial: %w", err)
	}
	result, err := fn(ctx, conn, argsForInvoker(inv))
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &dispatch.Response{Format: "json", StatusCode: http.StatusOK}, nil
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("grpc adapter: marshal response: %w", err)
	}
	return &dispatch.Response{
		Body:       body,
		Format:     "json",
		BytesOut:   len(body),
		StatusCode: http.StatusOK,
	}, nil
}

func bindingKey(rv *dispatch.ResolvedVariant) string {
	if rv == nil || rv.Variant == nil || rv.Variant.Binding == nil {
		return ""
	}
	return rv.Variant.Binding.AdapterKey
}

func argsForInvoker(inv *dispatch.Invocation) map[string]any {
	if inv == nil || inv.Args == nil {
		return map[string]any{}
	}
	return inv.Args
}

// Compile-time check that Adapter satisfies dispatch.Adapter.
var _ dispatch.Adapter = (*Adapter)(nil)
