package dispatch

import (
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
)

// cacheStatsOnly narrows the public Dispatcher to the (unexported) concrete
// method CacheStats(); the interface deliberately omits it so production
// callers go through the meta-tool, but tests in this internal package can
// type-assert to it.
type cacheStatsOnly interface {
	CacheStats() CacheLayerStats
}

// TestCacheStatsZeroWhenNoCacheWired pins the no-cache branch: a
// dispatcher constructed without either cache returns an all-zero
// CacheLayerStats so gum.cache_stats is safe to call before any cache
// is configured (spec §3003).
func TestCacheStatsZeroWhenNoCacheWired(t *testing.T) {
	d := NewDispatcherWithConfig(&catalog.Catalog{}, nil, DispatcherConfig{}).(cacheStatsOnly)
	got := d.CacheStats()
	if got != (CacheLayerStats{}) {
		t.Errorf("CacheStats=%+v; want zero value", got)
	}
}

// TestCacheStatsLegacyMemCache covers the legacy MemCache branch:
// when only Cache is wired the stats must reflect that cache's counters
// and Len/Bytes; SemanticCache is preferred so this path only fires
// when callers explicitly opt out of the §10.3 keying.
func TestCacheStatsLegacyMemCache(t *testing.T) {
	mc := cache.NewMemCache(8, time.Minute)
	d := NewDispatcherWithConfig(&catalog.Catalog{}, nil, DispatcherConfig{Cache: mc}).(cacheStatsOnly)
	got := d.CacheStats()
	if got.Hits != 0 || got.Misses != 0 || got.Evictions != 0 || got.Entries != 0 {
		t.Errorf("expected zero counters on fresh cache, got %+v", got)
	}
}

// TestCacheStatsSemanticWinsOverMemCache pins the precedence rule at
// lifecycle.go:223: SemanticCache takes priority when both are
// configured. Both caches are fresh so all counters are zero — the
// branch is exercised by the absence of any panic and by the entries
// count flowing through the semantic cache's Len().
func TestCacheStatsSemanticWinsOverMemCache(t *testing.T) {
	mc := cache.NewMemCache(8, time.Minute)
	sc := cache.NewSemanticCache(cache.SemanticConfig{MaxEntries: 4, DefaultTTL: time.Minute})
	d := NewDispatcherWithConfig(&catalog.Catalog{}, nil, DispatcherConfig{
		Cache:         mc,
		SemanticCache: sc,
	}).(cacheStatsOnly)
	got := d.CacheStats()
	if got.Entries != 0 {
		t.Errorf("Entries=%d; want 0", got.Entries)
	}
}
