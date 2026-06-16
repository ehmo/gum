package dispatch_test

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/dispatch"
)

// stubAuth returns fixed credentials with a non-empty subject fingerprint,
// standing in for an authenticated (byo_oauth/adc) principal.
type stubAuth struct{ fp string }

func (s stubAuth) ResolveAuth(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant) (*dispatch.Credentials, error) {
	return &dispatch.Credentials{Token: "tok", SubjectFingerprint: s.fp}, nil
}

func authedInv(opID string) *dispatch.Invocation {
	return &dispatch.Invocation{OpID: opID, Args: map[string]any{}, Format: "json"}
}

// TestSemanticCacheHitsForAuthenticatedCalls pins gum-vd63.1: with auth resolved
// before the cache lookup, an authenticated read (non-empty subject fingerprint)
// hits the §10.3 semantic cache on the second identical call. Before the fix the
// step-4 lookup keyed on "" while the step-7b store keyed on the fingerprint, so
// the adapter ran on every call (the cache was inert for every authed user).
func TestSemanticCacheHitsForAuthenticatedCalls(t *testing.T) {
	const opID = "test.cache.authed"
	const adapterKey = "test.adapter"
	adapter := &countingAdapter{}

	disp := dispatch.NewDispatcherWithConfig(minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{
			SemanticCache: cache.NewSemanticCache(cache.SemanticConfig{MaxEntries: 16}),
			Auth:          stubAuth{fp: "principal-A-fingerprint"},
		},
	)

	if _, err := disp.Dispatch(context.Background(), authedInv(opID)); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if got := adapter.calls.Load(); got != 1 {
		t.Fatalf("after first call: adapter.calls = %d, want 1", got)
	}
	// Second identical authenticated call must HIT (adapter not re-run).
	if _, err := disp.Dispatch(context.Background(), authedInv(opID)); err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}
	if got := adapter.calls.Load(); got != 1 {
		t.Errorf("after second call: adapter.calls = %d, want 1 (cache should have hit)", got)
	}
}

// TestSemanticCacheIsolatesPrincipals proves the auth-subject fingerprint still
// partitions the key: principal B sharing the same cache must NOT receive
// principal A's cached response.
func TestSemanticCacheIsolatesPrincipals(t *testing.T) {
	const opID = "test.cache.iso"
	const adapterKey = "test.adapter"
	snap := minimalCatalogFor(opID, adapterKey)
	adapter := &countingAdapter{}
	sc := cache.NewSemanticCache(cache.SemanticConfig{MaxEntries: 16})

	dispFor := func(fp string) dispatch.Dispatcher {
		return dispatch.NewDispatcherWithConfig(snap,
			map[string]dispatch.Adapter{adapterKey: adapter},
			dispatch.DispatcherConfig{SemanticCache: sc, Auth: stubAuth{fp: fp}},
		)
	}

	if _, err := dispFor("principal-A").Dispatch(context.Background(), authedInv(opID)); err != nil {
		t.Fatalf("principal A: %v", err)
	}
	if _, err := dispFor("principal-B").Dispatch(context.Background(), authedInv(opID)); err != nil {
		t.Fatalf("principal B: %v", err)
	}
	if got := adapter.calls.Load(); got != 2 {
		t.Errorf("adapter.calls = %d, want 2 — principal B must not get A's cached response", got)
	}
}
