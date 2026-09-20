package cache

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// dropBucket deletes the cache bucket out from under an open cache, which is
// what a hand-edited or partially restored cache file looks like.
func dropBucket(t *testing.T, c *BBoltCache) {
	t.Helper()
	if err := c.db.Update(func(tx *bolt.Tx) error { return tx.DeleteBucket(cacheBucket) }); err != nil {
		t.Fatalf("delete bucket: %v", err)
	}
}

func openTestCache(t *testing.T) *BBoltCache {
	t.Helper()
	c, err := Open(BBoltConfig{Path: filepath.Join(t.TempDir(), "cache.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// A cache file whose bucket is gone must read as empty and refuse writes,
// rather than panic on a nil bucket.
func TestMissingBucketDegradesToAMiss(t *testing.T) {
	c := openTestCache(t)
	if err := c.Set("k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("seed set: %v", err)
	}
	dropBucket(t, c)

	// The hot tier still holds the key, so the read has to bypass it to reach
	// the bbolt lookup under test.
	c.mu.Lock()
	c.hot = map[string]*hotEntry{}
	c.hotOrder = nil
	c.mu.Unlock()

	if payload, ok := c.Get("k"); ok {
		t.Errorf("Get = %q, true; want a miss when the bucket is gone", payload)
	}

	err := c.Set("k2", []byte("v"), time.Minute)
	if !errors.Is(err, errBucketMissing) {
		t.Errorf("Set err = %v, want errBucketMissing", err)
	}

	n, err := c.EvictExpired()
	if err != nil || n != 0 {
		t.Errorf("EvictExpired = (%d, %v), want (0, nil)", n, err)
	}

	if err := c.evictIfOverSize(); err != nil {
		t.Errorf("evictIfOverSize err = %v, want nil", err)
	}
}

// A closed cache must report the scan failure instead of reporting zero
// expired entries, which a caller would read as a healthy empty cache.
func TestEvictExpiredOnAClosedCacheReportsTheScanFailure(t *testing.T) {
	c := openTestCache(t)
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	n, err := c.EvictExpired()
	if err == nil || !strings.Contains(err.Error(), "scan for expired entries") {
		t.Fatalf("EvictExpired err = %v, want the scan failure", err)
	}
	if n != 0 {
		t.Errorf("deleted = %d, want 0", n)
	}
}

// A negative cap makes every total "over size" while there is nothing to
// evict. The sweep has to return cleanly rather than loop or error.
func TestEvictIfOverSizeWithNothingToEvict(t *testing.T) {
	c, err := Open(BBoltConfig{Path: filepath.Join(t.TempDir(), "cache.db"), MaxSizeBytes: -1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.evictIfOverSize(); err != nil {
		t.Fatalf("evictIfOverSize err = %v, want nil", err)
	}
}

// promoteToHot trims the hot tier by walking hotOrder. If the two ever
// disagree about their contents the loop must still terminate, because it runs
// under the cache write lock on every Set.
func TestPromoteToHotStopsWhenTheLRUOrderIsEmpty(t *testing.T) {
	c := openTestCache(t)

	c.mu.Lock()
	c.cfg.HotTierSize = 1
	c.hot = map[string]*hotEntry{"a": {}, "b": {}}
	c.hotOrder = nil
	c.promoteToHot("a", []byte("payload"), 0, time.Now().Unix())
	got := len(c.hot)
	c.mu.Unlock()

	if got != 2 {
		t.Errorf("hot entries = %d, want 2 left in place when hotOrder is empty", got)
	}
}

// A lastAccess stamp in the future (a clock step back) makes recency zero and
// the score infinite. VAAC has to report 0 so the entry is evicted first
// instead of ranking above everything else.
func TestVAACScoreRejectsAnInfiniteScore(t *testing.T) {
	now := time.Now()
	s := &SemanticCache{perOpTTL: map[string]time.Duration{}}
	rec := &semanticEntry{
		value:      []byte("v"),
		lastAccess: now.Add(time.Second),
		insertedAt: now,
	}

	if got := s.vaacScore(rec, now); got != 0 {
		t.Errorf("vaacScore = %v, want 0 for a non-finite score", got)
	}
	if !math.IsInf(1/(now.Sub(rec.lastAccess).Seconds()+1.0), 0) {
		t.Fatal("the fixture no longer produces a zero recency")
	}
}

// Migration carries a BBoltCache record across verbatim, and anything else as
// an opaque blob. The discriminator has to reject both a non-JSON value and a
// JSON document whose declared size disagrees with its payload.
func TestDecodeCacheRecordRejectsNonRecords(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"not_json", "not json at all"},
		{"no_payload", `{"size":3}`},
		{"size_disagrees", `{"payload":"aGk=","size":99}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := decodeCacheRecord([]byte(tc.in)); ok {
				t.Errorf("decodeCacheRecord(%q) = true, want false", tc.in)
			}
		})
	}

	if rec, ok := decodeCacheRecord([]byte(`{"payload":"aGk=","size":2,"expires_at_unix":7}`)); !ok {
		t.Error("a well-formed record was rejected")
	} else if string(rec.Payload) != "hi" || rec.ExpiresAtUnix != 7 {
		t.Errorf("record = %+v, want payload \"hi\" and expiry 7", rec)
	}
}

// The migration transaction is the only thing standing between a partial copy
// and a cache that looks complete. Every way of finishing it twice, or of
// using it after it is finished, has to fail loudly.
func TestImportRefusesUseAfterItIsFinished(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http-wal.db")
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	im, err := s.BeginImport()
	if err != nil {
		t.Fatalf("begin import: %v", err)
	}
	if err := im.Add("k", []byte("v"), 0); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := im.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if err := im.Rollback(); err != nil {
		t.Errorf("Rollback after Commit = %v, want nil so callers can defer it", err)
	}
	if err := im.Commit(); err == nil || !strings.Contains(err.Error(), "already finished") {
		t.Errorf("second Commit = %v, want the already-finished refusal", err)
	}
	if err := im.Add("k2", []byte("v"), 0); err == nil {
		t.Error("Add after Commit = nil, want the closed-statement failure")
	}
}

// A transaction that was rolled back underneath the Import must not report a
// successful commit, and a second rollback must report the driver's refusal
// rather than swallow it.
func TestImportSurfacesADeadTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http-wal.db")
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	commitSide, err := s.BeginImport()
	if err != nil {
		t.Fatalf("begin import: %v", err)
	}
	if err := commitSide.tx.Rollback(); err != nil {
		t.Fatalf("external rollback: %v", err)
	}
	if err := commitSide.Commit(); err == nil {
		t.Error("Commit on a dead transaction = nil, want the sentinel write failure")
	}

	rollbackSide, err := s.BeginImport()
	if err != nil {
		t.Fatalf("begin import: %v", err)
	}
	if err := rollbackSide.tx.Rollback(); err != nil {
		t.Fatalf("external rollback: %v", err)
	}
	if err := rollbackSide.Rollback(); err == nil ||
		!strings.Contains(err.Error(), "roll back migration transaction") {
		t.Errorf("Rollback = %v, want the driver refusal", err)
	}
}

// BeginImport on a closed database has to fail at the transaction, not hand
// back an Import whose first Add panics.
func TestBeginImportOnAClosedDatabaseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http-wal.db")
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	im, err := s.BeginImport()
	if err == nil {
		_ = im.Rollback()
		t.Fatal("BeginImport = nil error, want the begin failure")
	}
	if !strings.Contains(err.Error(), "begin migration transaction") {
		t.Errorf("err = %v, want one naming the transaction", err)
	}
}

// A cache file that gum cannot write has to fail the open. Silently running
// against a read-only file would drop every later write on the floor.
func TestOpenSQLiteWALRejectsAReadOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http-wal.db")
	seed, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	// The table has to be absent, or the idempotent CREATE is a no-op that
	// never touches the file.
	if _, err := seed.db.Exec(`DROP TABLE kv`); err != nil {
		t.Fatalf("drop kv: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if _, err := OpenSQLiteWAL(SQLiteConfig{Path: path}); err == nil ||
		!strings.Contains(err.Error(), "create kv table") {
		t.Fatalf("OpenSQLiteWAL err = %v, want the kv table write failure", err)
	}
}

// seedMismatchedKV leaves a cache file whose kv table carries a schema gum
// does not recognise. The idempotent CREATE TABLE at open is a no-op against
// it, so every failure lands on the statement that needs the real columns.
func seedMismatchedKV(t *testing.T, path string) {
	t.Helper()
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if _, err := s.db.Exec(`DROP TABLE kv`); err != nil {
		t.Fatalf("drop kv: %v", err)
	}
	if _, err := s.db.Exec(`CREATE TABLE kv (other TEXT)`); err != nil {
		t.Fatalf("create mismatched kv: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}
}

// A sentinel check that fails is not the same answer as an absent sentinel.
// Reading it as "absent" would delete a cache file that gum cannot read.
func TestMigrateSurfacesASentinelCheckFailure(t *testing.T) {
	dir := t.TempDir()
	seedMismatchedKV(t, filepath.Join(dir, HTTPWALDBFile))

	res, err := Migrate(MigrateOptions{CacheDir: dir})
	if err == nil || !strings.Contains(err.Error(), "sentinel check") {
		t.Fatalf("Migrate err = %v, want the sentinel check failure", err)
	}
	if res != nil {
		t.Errorf("result = %+v, want nil alongside the error", res)
	}
	if !fileExists(filepath.Join(dir, HTTPWALDBFile)) {
		t.Error("the unreadable wal was deleted; it must survive for --force")
	}
}

// BeginImport has to fail at the prepare, not hand back an Import whose rows
// vanish one by one.
func TestBeginImportRejectsAMismatchedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), HTTPWALDBFile)
	seedMismatchedKV(t, path)

	s, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	im, err := s.BeginImport()
	if err == nil {
		_ = im.Rollback()
		t.Fatal("BeginImport = nil error, want the prepare failure")
	}
	if !strings.Contains(err.Error(), "prepare migration insert") {
		t.Errorf("err = %v, want one naming the prepared insert", err)
	}
}

// --force discards a corrupt cache file. When the discard itself fails the
// migration has to stop, because rebuilding onto the same path would fail
// again in the middle of the copy.
func TestMigrateForceReportsAFailedDiscard(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, HTTPWALDBFile)
	if err := os.MkdirAll(filepath.Join(walPath, "occupied"), 0o700); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := Migrate(MigrateOptions{CacheDir: dir, Force: true}); err == nil ||
		!strings.Contains(err.Error(), "delete corrupt wal") {
		t.Fatalf("Migrate err = %v, want the discard failure", err)
	}
}

// A corrupt row carries no size, so the over-size sweep has to skip it rather
// than evict on a total it cannot compute.
func TestEvictIfOverSizeSkipsCorruptRecords(t *testing.T) {
	c, err := Open(BBoltConfig{Path: filepath.Join(t.TempDir(), "cache.db"), MaxSizeBytes: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	if err := c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(cacheBucket).Put([]byte("corrupt"), []byte("not a record"))
	}); err != nil {
		t.Fatalf("seed corrupt row: %v", err)
	}
	if err := c.Set("good", []byte("payload"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}

	if err := c.evictIfOverSize(); err != nil {
		t.Fatalf("evictIfOverSize err = %v, want nil", err)
	}

	var corruptKept, goodKept bool
	if err := c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		corruptKept = b.Get([]byte("corrupt")) != nil
		goodKept = b.Get([]byte("good")) != nil
		return nil
	}); err != nil {
		t.Fatalf("view: %v", err)
	}
	if !corruptKept {
		t.Error("the corrupt row was evicted; the sweep must leave rows it cannot size")
	}
	if goodKept {
		t.Error("the over-size row survived the sweep")
	}
}
