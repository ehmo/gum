package cache_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
)

// TestBBoltSetTriggersEvictionWithSwapAndEarlyBreak pins two arms of
// evictIfOverSize (bbolt.go:332-407):
//   - the sort swap at lines 370-372 (LRU ordering of >=2 entries where
//     bbolt's key-ordered ForEach yields them in non-LRU order).
//   - the early break at lines 379-380 (eviction loop terminates once
//     totalSize drops back under cap rather than evicting everything).
//
// We seed three 10-byte entries with cap=25 in this order: "b" first,
// "a" second, "c" third — each separated by >1s so LastAccessUnix
// values differ at second granularity. bbolt's ForEach iterates by key
// (a, b, c), feeding evictIfOverSize entries in non-LRU order:
// [{a, t2}, {b, t1}, {c, t3}]. The first inner-loop comparison
// (t2 > t1) MUST swap, putting "b" (oldest) at index 0. The eviction
// loop then evicts only "b" (totalSize 30 → 20) and breaks early since
// 20 <= cap. We assert b is gone and a, c remain.
func TestBBoltSetTriggersEvictionWithSwapAndEarlyBreak(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	c, err := cache.Open(cache.BBoltConfig{Path: dbPath, MaxSizeBytes: 25})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	payload := []byte("0123456789") // 10 bytes
	// "b" first → oldest LastAccessUnix.
	if err := c.Set("b", payload, time.Minute); err != nil {
		t.Fatalf("Set b: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	// "a" second → middle. bbolt's ForEach will visit it before "b"
	// since iteration is key-sorted, forcing the sort swap.
	if err := c.Set("a", payload, time.Minute); err != nil {
		t.Fatalf("Set a: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	// "c" third → newest. This Set pushes totalSize to 30 > cap (25),
	// triggering evictIfOverSize. Only "b" should be evicted.
	if err := c.Set("c", payload, time.Minute); err != nil {
		t.Fatalf("Set c: %v", err)
	}

	if got, ok := c.Get("b"); ok || got != nil {
		t.Errorf("Get(b) = (%v, %v) after evict; want (nil, false)", got, ok)
	}
	if _, ok := c.Get("a"); !ok {
		t.Errorf("Get(a) = miss after evict; want hit (newer than b)")
	}
	if _, ok := c.Get("c"); !ok {
		t.Errorf("Get(c) = miss after evict; want hit (newest)")
	}
}
