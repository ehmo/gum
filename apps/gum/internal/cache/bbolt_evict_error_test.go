package cache

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// readOnlyCache returns a cache whose bbolt handle accepts View but rejects
// Update, so the delete transaction in a sweep fails while the scan that finds
// the keys still succeeds. That is the split the count and the error have to
// survive: entries were found, none were removed.
func readOnlyCache(t *testing.T, seed func(b *bolt.Bucket) error) *BBoltCache {
	t.Helper()

	path := filepath.Join(t.TempDir(), "c.db")
	c, err := Open(BBoltConfig{Path: path})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.update(func(tx *bolt.Tx) error {
		return seed(tx.Bucket(cacheBucket))
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := c.db.Close(); err != nil {
		t.Fatalf("close writable handle: %v", err)
	}

	ro, err := bolt.Open(path, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen read-only: %v", err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	c.db = ro
	return c
}

func putRecord(b *bolt.Bucket, key string, rec cacheRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return b.Put([]byte(key), data)
}

// TestEvictExpiredReportsDeleteFailure pins gum-fbst: the sweep must report the
// number of entries it removed and surface the commit failure, not the number
// it found. `gum cache clear --expired` prints the returned count as
// expired_removed, so a swallowed error told the operator that a still-present
// entry was gone.
func TestEvictExpiredReportsDeleteFailure(t *testing.T) {
	past := time.Now().Unix() - 60
	c := readOnlyCache(t, func(b *bolt.Bucket) error {
		return putRecord(b, "dead", cacheRecord{Payload: []byte("v"), ExpiresAtUnix: past, Size: 1})
	})

	n, err := c.EvictExpired()
	if err == nil {
		t.Fatal("EvictExpired returned nil error although the delete transaction could not commit")
	}
	if n != 0 {
		t.Errorf("EvictExpired removed count = %d; want 0, nothing was deleted", n)
	}

	var still bool
	_ = c.view(func(tx *bolt.Tx) error {
		still = tx.Bucket(cacheBucket).Get([]byte("dead")) != nil
		return nil
	})
	if !still {
		t.Error("entry disappeared from bbolt; the read-only handle should have refused the delete")
	}
	if _, ok := c.hot["dead"]; ok {
		t.Error("entry left in the hot tier is fine, but this seed never populated it")
	}
}

// TestEvictExpiredCountsOnlyDeletedEntries asserts the ordinary path still
// returns the removed count.
func TestEvictExpiredCountsOnlyDeletedEntries(t *testing.T) {
	c, err := Open(BBoltConfig{Path: filepath.Join(t.TempDir(), "c.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = c.Close() }()

	past := time.Now().Unix() - 60
	if err := c.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if err := putRecord(b, "dead", cacheRecord{Payload: []byte("v"), ExpiresAtUnix: past, Size: 1}); err != nil {
			return err
		}
		return putRecord(b, "live", cacheRecord{Payload: []byte("v"), ExpiresAtUnix: 0, Size: 1})
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	n, err := c.EvictExpired()
	if err != nil {
		t.Fatalf("EvictExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("EvictExpired = %d; want 1", n)
	}
}

// TestEvictOverSizeReportsDeleteFailure pins the second half of gum-fbst:
// evictIfOverSize swallowed the same commit error, so a cache that could no
// longer evict grew past MaxSizeBytes without telling anyone. Set forwards
// this error; that forwarding is not exercised here, because a bbolt handle
// that accepts the Put and rejects the Delete cannot be built without fault
// injection.
func TestEvictOverSizeReportsDeleteFailure(t *testing.T) {
	c := readOnlyCache(t, func(b *bolt.Bucket) error {
		if err := putRecord(b, "old", cacheRecord{Payload: []byte("xxxxxxxxxx"), Size: 10, LastAccessUnix: 1}); err != nil {
			return err
		}
		return putRecord(b, "new", cacheRecord{Payload: []byte("yyyyyyyyyy"), Size: 10, LastAccessUnix: 2})
	})
	c.cfg.MaxSizeBytes = 12

	if err := c.evictIfOverSize(); err == nil {
		t.Fatal("evictIfOverSize returned nil although the delete transaction could not commit")
	}

	var still bool
	_ = c.view(func(tx *bolt.Tx) error {
		still = tx.Bucket(cacheBucket).Get([]byte("old")) != nil
		return nil
	})
	if !still {
		t.Error("LRU entry disappeared; the read-only handle should have refused the delete")
	}
}

// TestEvictOverSizeUnderCapIsNoError asserts the ordinary path, where nothing
// needs evicting, still reports success.
func TestEvictOverSizeUnderCapIsNoError(t *testing.T) {
	c, err := Open(BBoltConfig{Path: filepath.Join(t.TempDir(), "c.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = c.Close() }()

	if err := c.Set("k", []byte("v"), time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.evictIfOverSize(); err != nil {
		t.Errorf("evictIfOverSize under the cap returned %v; want nil", err)
	}
}
