// Spec §10.3 acceptance for the dispatcher's wiring of the semantic
// response cache. Validates the bead-gum-76g contract:
//   gum.cache_stats returns non-zero semantic.hits after repeated identical
//   calls within TTL.
//
// Also pins principal scoping (auth_subject_fingerprint dimension) and the
// fields-dimension distinction so two callers requesting different field
// projections of the same upstream payload do NOT share a cache entry.

package dispatch_test

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
)

// TestSemanticCacheSkipsWriteResponses pins the audit fix: a write-class op's
// response is NOT stored in the semantic cache. Caching a write success could
// serve a stale "done" for a later identical-arg call that never actually ran.
func TestSemanticCacheSkipsWriteResponses(t *testing.T) {
	const opID = "test.cache.write.op"
	const adapterKey = "test.adapter"
	snap := minimalCatalogFor(opID, adapterKey)
	snap.Ops[0].Variants[0].RiskClass = catalog.RiskClassWrite
	adapter := &countingAdapter{}
	sem := cache.NewSemanticCache(cache.SemanticConfig{MaxEntries: 64, DefaultTTL: 5 * time.Minute})

	disp := dispatch.NewDispatcherWithConfig(snap,
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{SemanticCache: sem})

	inv := &dispatch.Invocation{OpID: opID, Args: map[string]any{}, Format: "json", AllowWrite: true}
	if _, err := disp.Dispatch(context.Background(), inv); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if sem.Len() != 0 {
		t.Errorf("semantic cache stored %d entries for a write op; want 0 (writes must not be cached)", sem.Len())
	}
}

// dispatcherWithSemanticStats is the structural seam the MCP cacheStatProvider
// uses to read live counters. Local interface keeps the test from importing
// the MCP package.
type dispatcherWithSemanticStats interface {
	dispatch.Dispatcher
	CacheStats() dispatch.CacheLayerStats
}

// TestSemanticCacheRepeatedCallsRegisterHit is the gum-76g acceptance:
// after two identical Dispatch calls within TTL, CacheStats.Hits must be ≥1
// AND the adapter must have been called exactly once (the second call was
// served from cache).
func TestSemanticCacheRepeatedCallsRegisterHit(t *testing.T) {
	const opID = "test.cache.semantic.op"
	const adapterKey = "test.adapter"
	snap := minimalCatalogFor(opID, adapterKey)
	adapter := &countingAdapter{}

	sem := cache.NewSemanticCache(cache.SemanticConfig{
		MaxEntries: 64,
		DefaultTTL: 5 * time.Minute,
	})

	disp := dispatch.NewDispatcherWithConfig(snap,
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{SemanticCache: sem})

	dsp, ok := disp.(dispatcherWithSemanticStats)
	if !ok {
		t.Fatalf("dispatcher does not surface CacheStats; semantic cache wiring incomplete")
	}

	inv := func() *dispatch.Invocation {
		return &dispatch.Invocation{OpID: opID, Args: map[string]any{}, Format: "json"}
	}
	if _, err := disp.Dispatch(context.Background(), inv()); err != nil {
		t.Fatalf("first Dispatch: %v", err)
	}
	if _, err := disp.Dispatch(context.Background(), inv()); err != nil {
		t.Fatalf("second Dispatch: %v", err)
	}
	if adapter.calls.Load() != 1 {
		t.Errorf("adapter.calls=%d after 2 dispatches; want 1 (second served from cache)", adapter.calls.Load())
	}
	stats := dsp.CacheStats()
	if stats.Hits == 0 {
		t.Errorf("CacheStats.Hits=0 after repeated identical call; want >=1 (bead acceptance)")
	}
	if stats.Entries == 0 {
		t.Errorf("CacheStats.Entries=0 after population; want >=1")
	}
}

// TestSemanticCachePrincipalScopingIsolatesEntries: two dispatches with the
// same op + args but different auth_subject_fingerprint values must not
// share a cache entry. Spec §10.3 + §10.0.1 require principal-keyed cache
// isolation so switching credentials never replays the prior subject's data.
func TestSemanticCachePrincipalScopingIsolatesEntries(t *testing.T) {
	const opID = "test.cache.principal.op"
	const adapterKey = "test.adapter"
	snap := minimalCatalogFor(opID, adapterKey)
	adapter := &countingAdapter{}

	sem := cache.NewSemanticCache(cache.SemanticConfig{
		MaxEntries: 64,
		DefaultTTL: 5 * time.Minute,
	})
	disp := dispatch.NewDispatcherWithConfig(snap,
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{SemanticCache: sem})

	subjectA := &dispatch.Invocation{
		OpID: opID, Args: map[string]any{}, Format: "json",
		AuthSubjectFingerprint: "fp-subject-a",
	}
	subjectB := &dispatch.Invocation{
		OpID: opID, Args: map[string]any{}, Format: "json",
		AuthSubjectFingerprint: "fp-subject-b",
	}
	if _, err := disp.Dispatch(context.Background(), subjectA); err != nil {
		t.Fatalf("subjectA Dispatch: %v", err)
	}
	if _, err := disp.Dispatch(context.Background(), subjectB); err != nil {
		t.Fatalf("subjectB Dispatch: %v", err)
	}
	if adapter.calls.Load() != 2 {
		t.Errorf("adapter.calls=%d; want 2 (cross-subject must not share entries)", adapter.calls.Load())
	}
}

// TestSemanticCacheFieldsDimensionIsolatesEntries: two dispatches with the
// same (op, args, subject) but different field-mask projections must
// produce distinct cache entries. Spec §10.3 line 2253 explicitly enumerates
// `fields` as part of the cache key.
func TestSemanticCacheFieldsDimensionIsolatesEntries(t *testing.T) {
	const opID = "test.cache.fields.op"
	const adapterKey = "test.adapter"
	snap := minimalCatalogFor(opID, adapterKey)
	adapter := &countingAdapter{}

	sem := cache.NewSemanticCache(cache.SemanticConfig{
		MaxEntries: 64,
		DefaultTTL: 5 * time.Minute,
	})
	disp := dispatch.NewDispatcherWithConfig(snap,
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{SemanticCache: sem})

	invSlim := &dispatch.Invocation{
		OpID: opID, Args: map[string]any{}, Format: "json",
		OutputProfile: &profile.Profile{Projection: []string{"id"}},
	}
	invFat := &dispatch.Invocation{
		OpID: opID, Args: map[string]any{}, Format: "json",
		OutputProfile: &profile.Profile{Projection: []string{"id", "from", "subject", "snippet"}},
	}
	if _, err := disp.Dispatch(context.Background(), invSlim); err != nil {
		t.Fatalf("slim Dispatch: %v", err)
	}
	if _, err := disp.Dispatch(context.Background(), invFat); err != nil {
		t.Fatalf("fat Dispatch: %v", err)
	}
	if adapter.calls.Load() != 2 {
		t.Errorf("adapter.calls=%d; want 2 (different projections must not share entries)", adapter.calls.Load())
	}
}

// TestSemanticCacheConcurrentDispatchRaceFree fires many concurrent Dispatch
// calls through a dispatcher WITH the semantic cache wired (the production
// config) for the same read op, so they race on the cache's Get/Set. Run with
// -race it guards the hot per-call cache path against data races — a gap left by
// TestDispatchConcurrentRequestIDsUnique, which wires no cache.
func TestSemanticCacheConcurrentDispatchRaceFree(t *testing.T) {
	const opID = "test.cache.concurrent.op"
	const adapterKey = "test.adapter"
	snap := minimalCatalogFor(opID, adapterKey)
	adapter := &countingAdapter{}
	sem := cache.NewSemanticCache(cache.SemanticConfig{MaxEntries: 64, DefaultTTL: 5 * time.Minute})
	disp := dispatch.NewDispatcherWithConfig(snap,
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{SemanticCache: sem})

	const goroutines = 48
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_, _ = disp.Dispatch(context.Background(), &dispatch.Invocation{
					OpID: opID, Args: map[string]any{}, Format: "json",
				})
			}
		}()
	}
	wg.Wait()
}

// TestSemanticCacheHitPopulatesStructuredContent is the audit (8th pass)
// regression: a cache HIT must return the same typed StructuredContent as the
// cold (shapeResponse) path. Before the fix the hit path left it nil, so an MCP
// client got structured content on the first call and nil on every cached
// repeat.
func TestSemanticCacheHitPopulatesStructuredContent(t *testing.T) {
	const opID = "test.cache.structured.op"
	const adapterKey = "test.adapter"
	snap := minimalCatalogFor(opID, adapterKey)
	adapter := &countingAdapter{}
	sem := cache.NewSemanticCache(cache.SemanticConfig{MaxEntries: 64, DefaultTTL: 5 * time.Minute})
	disp := dispatch.NewDispatcherWithConfig(snap,
		map[string]dispatch.Adapter{adapterKey: adapter},
		dispatch.DispatcherConfig{SemanticCache: sem})

	inv := func() *dispatch.Invocation {
		return &dispatch.Invocation{OpID: opID, Args: map[string]any{}, Format: "json"}
	}
	cold, err := disp.Dispatch(context.Background(), inv())
	if err != nil {
		t.Fatalf("cold Dispatch: %v", err)
	}
	warm, err := disp.Dispatch(context.Background(), inv())
	if err != nil {
		t.Fatalf("warm Dispatch: %v", err)
	}
	if adapter.calls.Load() != 1 {
		t.Fatalf("adapter.calls=%d; want 1 (warm must be served from cache)", adapter.calls.Load())
	}
	if cold.StructuredContent == nil {
		t.Error("cold StructuredContent is nil; want populated")
	}
	if warm.StructuredContent == nil {
		t.Fatal("warm (cache-hit) StructuredContent is nil; want the same typed content as cold")
	}
	if !reflect.DeepEqual(cold.StructuredContent, warm.StructuredContent) {
		t.Errorf("StructuredContent differs cold vs warm:\n cold=%#v\n warm=%#v", cold.StructuredContent, warm.StructuredContent)
	}
}
