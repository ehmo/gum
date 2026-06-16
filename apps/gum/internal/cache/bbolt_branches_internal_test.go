package cache

import (
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// TestGetCorruptBboltRecordTreatedAsMiss pins BBoltCache.Get's
// `json.Unmarshal err → return nil` arm (bbolt.go:174-176). When a
// previous gum version wrote a different schema, or a future version
// rolls back, the on-disk record may fail to unmarshal under the
// current shape. Get MUST treat that as a miss (NOT panic, NOT
// surface the err) so the dispatch fallback path can re-fetch
// upstream and overwrite the bad record.
func TestGetCorruptBboltRecordTreatedAsMiss(t *testing.T) {
	dir := t.TempDir()
	cfg := BBoltConfig{Path: filepath.Join(dir, "cache.db")}
	c, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// Inject a corrupt record directly via bbolt — the cacheRecord
	// shape requires the "payload" key as a byte slice; "{not json"
	// is malformed JSON entirely.
	if err := c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		return b.Put([]byte("corrupt.key"), []byte("{not json"))
	}); err != nil {
		t.Fatalf("inject corrupt: %v", err)
	}

	payload, ok := c.Get("corrupt.key")
	if ok {
		t.Errorf("Get(corrupt) ok=true, payload=%q; want ok=false (miss)", payload)
	}
}

// TestEvictExpiredDeletesCorruptRecords pins EvictExpired's
// `json.Unmarshal err → expiredKeys = append; return nil` arm
// (bbolt.go:259-262). Corrupt records are treated as expired and
// deleted so the cache self-heals across a schema rollback rather
// than carrying poison rows forever.
func TestEvictExpiredDeletesCorruptRecords(t *testing.T) {
	dir := t.TempDir()
	cfg := BBoltConfig{Path: filepath.Join(dir, "cache.db")}
	c, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		return b.Put([]byte("poison"), []byte("not a record"))
	}); err != nil {
		t.Fatalf("inject corrupt: %v", err)
	}

	if got := c.EvictExpired(); got != 1 {
		t.Errorf("EvictExpired()=%d; want 1 (corrupt row treated as expired)", got)
	}

	// Confirm the row is gone.
	_ = c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if got := b.Get([]byte("poison")); got != nil {
			t.Errorf("poison row still present after EvictExpired: %q", got)
		}
		return nil
	})
}

// TestGetHotTierUpdatesLastAccessAfter60s pins Get's lazy
// `now - lastAccess > 60 → update lastAccessUnix` arm
// (bbolt.go:143-148). The hot-tier LRU eviction order is driven by
// lastAccessUnix; without the periodic refresh, frequently-read
// entries would be evicted as if cold. We force the condition by
// pre-aging the hot entry's lastAccessUnix and then reading it.
func TestGetHotTierUpdatesLastAccessAfter60s(t *testing.T) {
	dir := t.TempDir()
	cfg := BBoltConfig{Path: filepath.Join(dir, "cache.db")}
	c, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.Set("k", []byte("v"), 5*time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// Pre-age the hot entry so the `now-lastAccess > 60` predicate fires.
	c.mu.Lock()
	he, ok := c.hot["k"]
	if !ok {
		c.mu.Unlock()
		t.Fatal("expected hot entry after Set; got none")
	}
	he.lastAccessUnix = time.Now().Unix() - 120
	staleAccess := he.lastAccessUnix
	c.mu.Unlock()

	if _, ok := c.Get("k"); !ok {
		t.Fatal("Get(k) ok=false; want hot-tier hit")
	}

	c.mu.RLock()
	updated := c.hot["k"].lastAccessUnix
	c.mu.RUnlock()
	if updated <= staleAccess {
		t.Errorf("lastAccessUnix=%d; want > staleAccess=%d (refresh did not fire)", updated, staleAccess)
	}
}
