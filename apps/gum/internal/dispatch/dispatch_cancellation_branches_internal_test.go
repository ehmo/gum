package dispatch

import (
	"context"
	"errors"
	"testing"
)

// authResolverFn adapts a closure to AuthResolver for inline test stubs.
type authResolverFn func(context.Context, *Invocation, *ResolvedVariant) (*Credentials, error)

func (f authResolverFn) ResolveAuth(ctx context.Context, inv *Invocation, rv *ResolvedVariant) (*Credentials, error) {
	return f(ctx, inv, rv)
}

// tokenBucketFn adapts a closure to TokenBucket for inline test stubs.
type tokenBucketFn func(context.Context, string, string) error

func (f tokenBucketFn) Wait(ctx context.Context, opID, credsID string) error {
	return f(ctx, opID, credsID)
}

// TestDispatchCheckCancelledAfterResolveAuth pins lifecycle.go:399-401 — the
// checkCancelled gate that runs *after* step 5 succeeds. The existing
// cancellation suite cancels via a blocking auth resolver that returns
// ctx.Err(), which trips the resolveAuth *error* arm instead. Here the auth
// resolver succeeds (returns valid creds, nil error) but cancels the context
// as a side effect, so the dedicated post-auth checkCancelled fires.
func TestDispatchCheckCancelledAfterResolveAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	auth := authResolverFn(func(_ context.Context, _ *Invocation, _ *ResolvedVariant) (*Credentials, error) {
		cancel() // resolve succeeds, but the context is now done
		return &Credentials{}, nil
	})
	adapters := map[string]Adapter{"noop": AdapterFunc(func(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error) {
		t.Error("adapter ran; checkCancelled after resolve_auth should have short-circuited")
		return &Response{Body: []byte("{}"), Format: "json", StatusCode: 200}, nil
	})}
	d := NewDispatcherWithConfig(minimalCatalog("noop"), adapters, DispatcherConfig{Auth: auth})

	_, err := d.Dispatch(ctx, &Invocation{OpID: "gum.code", Format: "json", RequestID: "auth-cancel"})
	if err == nil {
		t.Fatal("Dispatch err = nil; want cancellation after resolve_auth")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want wrapped context.Canceled", err)
	}
}

// TestDispatchCheckCancelledAfterTokenBucket pins lifecycle.go:410-412 — the
// checkCancelled gate after step 6. The blocking-bucket suite returns
// ctx.Err() (tripping the token-bucket error arm); here Wait succeeds (nil)
// but cancels the context, so the dedicated post-token-bucket checkCancelled
// fires before the adapter executes.
func TestDispatchCheckCancelledAfterTokenBucket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bucket := tokenBucketFn(func(_ context.Context, _, _ string) error {
		cancel() // slot granted, but the context is now done
		return nil
	})
	adapters := map[string]Adapter{"noop": AdapterFunc(func(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error) {
		t.Error("adapter ran; checkCancelled after token_bucket should have short-circuited")
		return &Response{Body: []byte("{}"), Format: "json", StatusCode: 200}, nil
	})}
	d := NewDispatcherWithConfig(minimalCatalog("noop"), adapters, DispatcherConfig{RateLimiter: bucket})

	_, err := d.Dispatch(ctx, &Invocation{OpID: "gum.code", Format: "json", RequestID: "bucket-cancel"})
	if err == nil {
		t.Fatal("Dispatch err = nil; want cancellation after token_bucket")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want wrapped context.Canceled", err)
	}
}

// TestDispatchResolveVariantErrorPropagates pins lifecycle.go:350-353 — the
// `serr3 != nil → logEvent + return` arm where a step-3 routing failure
// surfaces out of the full Dispatch path. An op whose only variant is
// quarantined (and no default matches an active variant) makes resolveVariant
// return VARIANT_QUARANTINED, which Dispatch must propagate verbatim.
func TestDispatchResolveVariantErrorPropagates(t *testing.T) {
	cat := minimalCatalog("noop")
	// Quarantine the only variant and drop the default so step-3 routing fails.
	cat.Ops[0].DefaultVariantID = ""
	cat.Ops[0].Variants[0].Quarantined = true

	adapters := map[string]Adapter{"noop": AdapterFunc(func(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error) {
		t.Error("adapter ran; resolveVariant failure should have short-circuited")
		return nil, nil
	})}
	d := NewDispatcher(cat, adapters)

	_, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json", RequestID: "variant-err"})
	if err == nil {
		t.Fatal("Dispatch err = nil; want VARIANT_QUARANTINED from step 3")
	}
	var se *StructuredError
	if !errors.As(err, &se) || se.ErrCode != ErrCodeVariantQuarantined {
		t.Errorf("err = %v; want StructuredError VARIANT_QUARANTINED", err)
	}
}
