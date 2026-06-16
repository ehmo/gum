package dispatch_test

import (
	"context"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestLegacyMemCacheBypassedWhenAuthConfigured pins the cross-account guard
// (review gum-t8x1): the deprecated MemCache path keys without a principal
// component, so when an auth resolver is configured the kernel must NOT use it.
// Concretely, two identical authenticated reads must both reach the adapter —
// a cache hit on the second call would prove the principal-blind legacy cache
// served a stored response, the cross-account-leak failure mode.
func TestLegacyMemCacheBypassedWhenAuthConfigured(t *testing.T) {
	const opID = "test.cache.authguard"
	const adapterKey = "test.adapter"
	adapter := &countingAdapter{}

	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: adapter},
		// Legacy MemCache + an auth resolver: the dangerous combination.
		dispatch.DispatcherConfig{
			Cache: cache.NewMemCache(100, time.Minute),
			Auth:  stubAuth{fp: "principal-A"},
		},
	)

	for i := 0; i < 2; i++ {
		if _, err := disp.Dispatch(context.Background(), authedInv(opID)); err != nil {
			t.Fatalf("Dispatch %d: %v", i, err)
		}
	}

	if got := adapter.calls.Load(); got != 2 {
		t.Errorf("adapter.calls = %d; want 2 — the principal-blind legacy MemCache "+
			"must be bypassed when auth is configured (a hit means cross-account serving)", got)
	}
}

// TestLegacyMemCacheStillCachesWithoutAuth is the control: with no auth
// resolver, the legacy MemCache remains usable (the bypass is scoped strictly
// to the authenticated case), so a second identical call is a cache hit.
func TestLegacyMemCacheStillCachesWithoutAuth(t *testing.T) {
	const opID = "test.cache.noauth"
	const adapterKey = "test.adapter"
	adapter := &countingAdapter{}

	disp := dispatch.NewDispatcherWithConfig(
		minimalCatalogFor(opID, adapterKey),
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{Cache: cache.NewMemCache(100, time.Minute)},
	)

	for i := 0; i < 2; i++ {
		if _, err := disp.Dispatch(context.Background(), authedInv(opID)); err != nil {
			t.Fatalf("Dispatch %d: %v", i, err)
		}
	}

	if got := adapter.calls.Load(); got != 1 {
		t.Errorf("adapter.calls = %d; want 1 — unauthenticated legacy cache should hit on the 2nd call", got)
	}
}
