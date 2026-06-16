package cache_test

import (
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/cache"
)

// TestBBoltCacheEvictExpired covers the eviction sweep across three
// scenarios: nothing to evict (returns 0), one expired key (returned;
// subsequent Get is a miss), and a mix of expired+live (only expired
// removed). The 1ms sleep is the cheapest way to guarantee Unix-second
// expiry has elapsed; bbolt-backed expiry uses int64 seconds so we set
// TTLs in the past via negative durations.
func TestBBoltCacheEvictExpired(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	c, err := cache.Open(cache.BBoltConfig{Path: filepath.Join(dir, "cache.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// Sub-test 1: nothing in the cache → 0 evictions.
	if got := c.EvictExpired(); got != 0 {
		t.Errorf("EvictExpired on empty cache = %d, want 0", got)
	}

	// Put one live entry and one short-TTL entry that will expire during
	// the test's sleep window. Set rounds the TTL to whole Unix seconds,
	// so we use 1s + a 1.1s sleep to guarantee the dead entry's
	// ExpiresAtUnix is strictly less than time.Now().Unix() at sweep
	// time.
	liveKey := cache.KeyFor("op.live", "{}", "scope", "cred")
	deadKey := cache.KeyFor("op.dead", "{}", "scope", "cred")
	if err := c.Set(liveKey, []byte("live"), time.Hour); err != nil {
		t.Fatalf("Set live: %v", err)
	}
	if err := c.Set(deadKey, []byte("dead"), time.Second); err != nil {
		t.Fatalf("Set dead: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)

	// One expired entry → one eviction.
	if got := c.EvictExpired(); got != 1 {
		t.Errorf("EvictExpired = %d, want 1 (only the dead entry)", got)
	}

	// Verify live entry survived; dead entry is gone.
	if _, ok := c.Get(liveKey); !ok {
		t.Error("live entry was evicted; want it to survive")
	}
	if _, ok := c.Get(deadKey); ok {
		t.Error("dead entry was not evicted; want it gone")
	}

	// Second sweep → 0 evictions (nothing left to expire).
	if got := c.EvictExpired(); got != 0 {
		t.Errorf("second sweep = %d, want 0", got)
	}
}
