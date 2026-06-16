package cache_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
)

// TestBBoltGetExpiredFromBBoltReturnsMiss pins Get's bbolt-path
// `record.ExpiresAtUnix != 0 && record.ExpiresAtUnix <= now → return (nil, false)`
// arm (bbolt.go:186-188). The hot-tier expiry branch is covered by
// existing tests; this one targets the case where the in-memory hot
// tier doesn't carry the key (e.g. after process restart) but the
// persisted bbolt record exists and has already expired. We reach it
// by Set-with-past-expiry then closing+reopening so the fresh
// BBoltCache has no hot entries.
func TestBBoltGetExpiredFromBBoltReturnsMiss(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "cache.db")

	c1, err := cache.Open(cache.BBoltConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// 1ns TTL is immediately expired by the time Get runs.
	if err := c1.Set("k", []byte("v"), time.Nanosecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c1.Close(); err != nil {
		t.Fatalf("Close c1: %v", err)
	}

	// Fresh cache → empty hot tier → Get falls through to bbolt.
	c2, err := cache.Open(cache.BBoltConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("Open c2: %v", err)
	}
	t.Cleanup(func() { _ = c2.Close() })

	// Sleep a tick to make sure the 1ns expiry is firmly in the past
	// across clock granularity quirks. 5ms is well within the Set→Get
	// gap a normal Cache test would tolerate.
	time.Sleep(5 * time.Millisecond)

	got, ok := c2.Get("k")
	if ok || got != nil {
		t.Errorf("Get(expired-bbolt) = (%v, %v); want (nil, false)", got, ok)
	}
}
