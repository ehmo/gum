package dispatch_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// catchPanic calls fn and returns ("panic: ...", true) if fn panics.
// Lets dispatch integration tests detect unimplemented stubs cleanly.
func catchPanic(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v", r)
			panicked = true
		}
	}()
	fn()
	return "", false
}

// countingAdapter is a dispatch.Adapter that counts how many times Execute is called.
type countingAdapter struct {
	calls atomic.Int32
}

func (a *countingAdapter) Execute(_ context.Context, _ *dispatch.Invocation, _ *dispatch.ResolvedVariant, _ *dispatch.Credentials) (*dispatch.Response, error) {
	a.calls.Add(1)
	return &dispatch.Response{
		Body:       []byte(`{"messages":[]}`),
		Format:     "json",
		StatusCode: 200,
		BytesOut:   16,
	}, nil
}

// minimalCatalogFor builds a minimal single-op catalog that routes op_id to adapterKey.
func minimalCatalogFor(opID, adapterKey string) *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops: []catalog.Op{
			{
				OpID:             opID,
				OpSchemaVersion:  1,
				Title:            "Test op",
				Summary:          "Test op for cache integration test.",
				DefaultVariantID: "v1",
				Variants: []catalog.Variant{
					{
						VariantID:     "v1",
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindDiscoveryREST,
						BackendKind:   catalog.BackendKindTypedRestSDK,
						RiskClass:     catalog.RiskClassRead,
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           adapterKey,
							OperationKey:         opID,
						},
					},
				},
			},
		},
	}
}

// TestKernelHonorsCacheHit verifies (G3.7 + TestReadOpCacheHit):
// when the cache returns a hit on the second call for the same op, the adapter's
// Execute method is NOT called a second time.
func TestKernelHonorsCacheHit(t *testing.T) {
	defer goleak.VerifyNone(t)

	const opID = "test.cache.op"
	const adapterKey = "test.adapter"

	snap := minimalCatalogFor(opID, adapterKey)
	adapter := &countingAdapter{}

	var mc *cache.MemCache
	msg, panicked := catchPanic(func() {
		mc = cache.NewMemCache(100, time.Minute)
	})
	if panicked {
		t.Fatalf("cache.NewMemCache panicked: %s — green team must implement NewMemCache", msg)
	}

	cfg := dispatch.DispatcherConfig{
		Cache: mc,
	}

	var disp dispatch.Dispatcher
	msg, panicked = catchPanic(func() {
		disp = dispatch.NewDispatcherWithConfig(snap, map[string]dispatch.Adapter{
			adapterKey: adapter,
		}, cfg)
	})
	if panicked {
		t.Fatalf("dispatch.NewDispatcherWithConfig panicked: %s — green team must implement NewDispatcherWithConfig", msg)
	}

	inv := &dispatch.Invocation{
		OpID:      opID,
		Args:      map[string]any{},
		Format:    "json",
		RequestID: "cache-test-1",
	}

	// First call: should reach the adapter.
	var resp1 *dispatch.ShapedResponse
	var err error
	msg, panicked = catchPanic(func() {
		resp1, err = disp.Dispatch(context.Background(), inv)
	})
	if panicked {
		t.Fatalf("first Dispatch panicked: %s", msg)
	}
	if err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if resp1 == nil {
		t.Fatal("first Dispatch: nil response")
	}
	if adapter.calls.Load() != 1 {
		t.Errorf("after first call: adapter.calls = %d, want 1", adapter.calls.Load())
	}

	// Second call with same args within the TTL: must be a cache hit.
	inv2 := &dispatch.Invocation{
		OpID:      opID,
		Args:      map[string]any{},
		Format:    "json",
		RequestID: "cache-test-2",
	}
	var resp2 *dispatch.ShapedResponse
	msg, panicked = catchPanic(func() {
		resp2, err = disp.Dispatch(context.Background(), inv2)
	})
	if panicked {
		t.Fatalf("second Dispatch panicked: %s", msg)
	}
	if err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}
	if resp2 == nil {
		t.Fatal("second Dispatch: nil response")
	}
	if adapter.calls.Load() != 1 {
		t.Errorf("after second call (cache hit): adapter.calls = %d, want 1 (executor must NOT be called again)", adapter.calls.Load())
	}
}

// TestCacheKeyIncludesVariantID verifies variant_id participates in the cache
// key (spec §10 line 2151). Two ops with the same op_id-shape but different
// default variants must NOT alias to the same entry. The dispatch lifecycle's
// resolveVariant uses op.DefaultVariantID; this test exercises that path by
// constructing two ops that differ only in their default variant id.
func TestCacheKeyIncludesVariantID(t *testing.T) {
	defer goleak.VerifyNone(t)
	const adapterKey = "test.adapter"

	mkCatalog := func(opID, variantID string) *catalog.Catalog {
		c := minimalCatalogFor(opID, adapterKey)
		c.Ops[0].Variants[0].VariantID = variantID
		c.Ops[0].DefaultVariantID = variantID
		return c
	}

	// Two distinct catalogs, same op_id, same args — but different variant ids.
	cat1 := mkCatalog("test.cache.shared", "vA")
	cat2 := mkCatalog("test.cache.shared", "vB")
	adapter1 := &countingAdapter{}
	adapter2 := &countingAdapter{}
	mc := cache.NewMemCache(100, time.Minute)

	d1 := dispatch.NewDispatcherWithConfig(cat1,
		map[string]dispatch.Adapter{adapterKey: adapter1},
		dispatch.DispatcherConfig{Cache: mc})
	d2 := dispatch.NewDispatcherWithConfig(cat2,
		map[string]dispatch.Adapter{adapterKey: adapter2},
		dispatch.DispatcherConfig{Cache: mc})

	inv := func() *dispatch.Invocation {
		return &dispatch.Invocation{OpID: "test.cache.shared", Args: map[string]any{}, Format: "json"}
	}

	// vA dispatch: populates cache under (op_id="test.cache.shared", variant_id="vA").
	if _, err := d1.Dispatch(context.Background(), inv()); err != nil {
		t.Fatalf("d1 first Dispatch: %v", err)
	}
	if got := adapter1.calls.Load(); got != 1 {
		t.Fatalf("d1 first: adapter1.calls=%d want 1", got)
	}

	// vB dispatch with same op/args: MUST miss because variant_id is different
	// (would erroneously hit if variant_id were missing from the key).
	if _, err := d2.Dispatch(context.Background(), inv()); err != nil {
		t.Fatalf("d2 first Dispatch: %v", err)
	}
	if got := adapter2.calls.Load(); got != 1 {
		t.Errorf("d2 first: adapter2.calls=%d want 1 (different variant_id must miss)", got)
	}

	// vA again: MUST hit (same op + args + variant_id as the first call).
	if _, err := d1.Dispatch(context.Background(), inv()); err != nil {
		t.Fatalf("d1 second Dispatch: %v", err)
	}
	if got := adapter1.calls.Load(); got != 1 {
		t.Errorf("d1 second: adapter1.calls=%d want 1 (vA entry must still be hot)", got)
	}
}

// TestCacheDisabledWhenNilNoLookups verifies d.cache==nil yields no caching:
// every Dispatch reaches the adapter, even with identical args.
func TestCacheDisabledWhenNilNoLookups(t *testing.T) {
	defer goleak.VerifyNone(t)

	const opID = "test.cache.disabled"
	const adapterKey = "test.adapter"
	snap := minimalCatalogFor(opID, adapterKey)
	adapter := &countingAdapter{}

	// No Cache in config → d.cache is nil → cacheCheck returns false.
	disp := dispatch.NewDispatcherWithConfig(snap, map[string]dispatch.Adapter{
		adapterKey: adapter,
	}, dispatch.DispatcherConfig{})

	for i := 0; i < 3; i++ {
		if _, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
			OpID: opID, Args: map[string]any{}, Format: "json",
		}); err != nil {
			t.Fatalf("Dispatch #%d: %v", i, err)
		}
	}
	if got := adapter.calls.Load(); got != 3 {
		t.Errorf("disabled cache: adapter.calls=%d want 3 (every call must reach adapter)", got)
	}
}
