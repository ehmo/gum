// Spec §10.2 acceptance for the BoltDB → WAL-SQLite migration tool.
// Covers all 4 branches in Migrate's docstring, idempotency, the rsync
// ambiguity gate + --force override, and the sub-bucket warning path.

package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// seedBolt creates a minimal BoltDB at path with the provided
// bucket→key→value triples. Sub-buckets can be added via subKeys.
func seedBolt(t *testing.T, path string, top map[string]map[string]string, subBuckets map[string]map[string]map[string]string) {
	t.Helper()
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("seedBolt open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Update(func(tx *bolt.Tx) error {
		for bn, kv := range top {
			b, err := tx.CreateBucketIfNotExists([]byte(bn))
			if err != nil {
				return err
			}
			for k, v := range kv {
				if err := b.Put([]byte(k), []byte(v)); err != nil {
					return err
				}
			}
		}
		for bn, subs := range subBuckets {
			parent, err := tx.CreateBucketIfNotExists([]byte(bn))
			if err != nil {
				return err
			}
			for sname, kv := range subs {
				sb, err := parent.CreateBucketIfNotExists([]byte(sname))
				if err != nil {
					return err
				}
				for k, v := range kv {
					if err := sb.Put([]byte(k), []byte(v)); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seedBolt update: %v", err)
	}
}

// TestMigrateRequiresCacheDir pins the empty-CacheDir guard.
func TestMigrateRequiresCacheDir(t *testing.T) {
	if _, err := Migrate(MigrateOptions{}); err == nil {
		t.Fatal("Migrate with empty CacheDir returned nil error")
	}
}

// TestMigrateBranch4FreshBootstrap: neither file present → create empty
// http-wal.db with sentinel.
func TestMigrateBranch4FreshBootstrap(t *testing.T) {
	dir := t.TempDir()
	res, err := Migrate(MigrateOptions{CacheDir: dir})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.BoltExisted || res.WALExisted {
		t.Errorf("BoltExisted/WALExisted = %v/%v; want false/false", res.BoltExisted, res.WALExisted)
	}
	if !res.SentinelWritten {
		t.Error("SentinelWritten=false on fresh bootstrap; want true")
	}
	if res.EntriesMigrated != 0 || res.BakRenamed {
		t.Errorf("unexpected mutations: entries=%d bakRenamed=%v", res.EntriesMigrated, res.BakRenamed)
	}
	// Confirm sentinel persisted.
	walPath := filepath.Join(dir, HTTPWALDBFile)
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: walPath})
	if err != nil {
		t.Fatalf("post-migrate open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ok, _ := s.SentinelPresent()
	if !ok {
		t.Error("sentinel absent after fresh bootstrap")
	}
}

// TestMigrateBranch3BoltToWAL: bolt exists, wal does not → copy + sentinel
// + rename to .bak.
func TestMigrateBranch3BoltToWAL(t *testing.T) {
	dir := t.TempDir()
	boltPath := filepath.Join(dir, HTTPCacheBoltFile)
	seedBolt(t, boltPath, map[string]map[string]string{
		"gum-cache": {"k1": "v1", "k2": "v2"},
		"meta":      {"foo": "bar"},
	}, nil)
	res, err := Migrate(MigrateOptions{CacheDir: dir})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !res.BoltExisted || res.WALExisted {
		t.Errorf("BoltExisted/WALExisted = %v/%v; want true/false", res.BoltExisted, res.WALExisted)
	}
	if res.EntriesMigrated != 3 {
		t.Errorf("EntriesMigrated=%d; want 3", res.EntriesMigrated)
	}
	if !res.SentinelWritten {
		t.Error("SentinelWritten=false")
	}
	if !res.BakRenamed {
		t.Error("BakRenamed=false; spec mandates rename after copy")
	}
	if _, err := os.Stat(filepath.Join(dir, HTTPCacheBoltBakFile)); err != nil {
		t.Errorf("expected http.db.bak after migration: %v", err)
	}
	if _, err := os.Stat(boltPath); !os.IsNotExist(err) {
		t.Errorf("http.db still present post-rename: %v", err)
	}
	// Verify a migrated row.
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	defer func() { _ = s.Close() }()
	got, ok, _ := s.Get("gum-cache/k1")
	if !ok || string(got) != "v1" {
		t.Errorf("Get(gum-cache/k1)=(%q,%v); want (v1,true)", got, ok)
	}
}

// TestMigrateBranch1SentinelPresent: wal already has sentinel → no-op,
// rename any leftover bolt to .bak.
func TestMigrateBranch1SentinelPresent(t *testing.T) {
	dir := t.TempDir()
	// Prime wal with sentinel.
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("prime wal: %v", err)
	}
	if err := s.WriteSentinel(); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}
	_ = s.Close()
	// Drop a leftover bolt.
	seedBolt(t, filepath.Join(dir, HTTPCacheBoltFile), map[string]map[string]string{
		"x": {"a": "b"},
	}, nil)

	res, err := Migrate(MigrateOptions{CacheDir: dir})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !res.SentinelPresent {
		t.Error("SentinelPresent (pre)=false; want true")
	}
	if res.SentinelWritten {
		t.Error("SentinelWritten=true on no-op branch; want false")
	}
	if res.EntriesMigrated != 0 {
		t.Errorf("EntriesMigrated=%d; want 0 on no-op", res.EntriesMigrated)
	}
	if !res.BakRenamed {
		t.Error("BakRenamed=false; expected leftover bolt to be renamed")
	}
	if _, err := os.Stat(filepath.Join(dir, HTTPCacheBoltBakFile)); err != nil {
		t.Errorf("http.db.bak missing: %v", err)
	}
}

// TestMigrateBranch2MidCrashRecovery: wal present but no sentinel, no bolt
// → delete wal, fall through to branch 4 (fresh bootstrap).
func TestMigrateBranch2MidCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	// Create wal without sentinel.
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("seed empty wal: %v", err)
	}
	if err := s.Set("orphan", []byte("data"), 0); err != nil {
		t.Fatalf("Set orphan: %v", err)
	}
	_ = s.Close()

	res, err := Migrate(MigrateOptions{CacheDir: dir})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !res.WALExisted {
		t.Error("WALExisted=false; want true (we observed the pre-existing wal)")
	}
	if !res.SentinelWritten {
		t.Error("SentinelWritten=false; want true (fresh bootstrap after wal deletion)")
	}
	// Orphan data must be gone.
	s2, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("post open: %v", err)
	}
	defer func() { _ = s2.Close() }()
	if _, ok, _ := s2.Get("orphan"); ok {
		t.Error("orphan key survived mid-crash recovery")
	}
}

// TestMigrateRsyncAmbiguityWithoutForce: both files present, no sentinel
// → ErrRsyncAmbiguity, no mutation.
func TestMigrateRsyncAmbiguityWithoutForce(t *testing.T) {
	dir := t.TempDir()
	// Bolt + wal without sentinel.
	seedBolt(t, filepath.Join(dir, HTTPCacheBoltFile), map[string]map[string]string{
		"x": {"a": "b"},
	}, nil)
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("seed wal: %v", err)
	}
	_ = s.Close()

	_, err = Migrate(MigrateOptions{CacheDir: dir})
	if !errors.Is(err, ErrRsyncAmbiguity) {
		t.Fatalf("err=%v; want ErrRsyncAmbiguity", err)
	}
	// No mutation: bolt still present.
	if _, statErr := os.Stat(filepath.Join(dir, HTTPCacheBoltFile)); statErr != nil {
		t.Errorf("bolt removed on ambiguity error: %v", statErr)
	}
}

// TestMigrateRsyncAmbiguityWithForce: --force deletes wal, retries.
func TestMigrateRsyncAmbiguityWithForce(t *testing.T) {
	dir := t.TempDir()
	seedBolt(t, filepath.Join(dir, HTTPCacheBoltFile), map[string]map[string]string{
		"b": {"k": "v"},
	}, nil)
	s, _ := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	_ = s.Close()

	res, err := Migrate(MigrateOptions{CacheDir: dir, Force: true})
	if err != nil {
		t.Fatalf("Migrate(force): %v", err)
	}
	if !res.SentinelWritten {
		t.Error("SentinelWritten=false after --force")
	}
	if res.EntriesMigrated != 1 {
		t.Errorf("EntriesMigrated=%d; want 1 (single bolt entry)", res.EntriesMigrated)
	}
	if !res.BakRenamed {
		t.Error("BakRenamed=false; expected rename after --force re-migration")
	}
}

// TestMigrateIdempotency: running Migrate twice after a successful first
// run must be a no-op (spec §10.2: idempotent).
func TestMigrateIdempotency(t *testing.T) {
	dir := t.TempDir()
	seedBolt(t, filepath.Join(dir, HTTPCacheBoltFile), map[string]map[string]string{
		"b": {"k": "v"},
	}, nil)
	first, err := Migrate(MigrateOptions{CacheDir: dir})
	if err != nil {
		t.Fatalf("Migrate #1: %v", err)
	}
	if !first.SentinelWritten {
		t.Fatal("first run did not write sentinel")
	}
	second, err := Migrate(MigrateOptions{CacheDir: dir})
	if err != nil {
		t.Fatalf("Migrate #2: %v", err)
	}
	if !second.SentinelPresent {
		t.Error("second run reports SentinelPresent=false; want true")
	}
	if second.SentinelWritten {
		t.Error("second run re-wrote sentinel; want no-op")
	}
	if second.EntriesMigrated != 0 {
		t.Errorf("second run migrated %d; want 0", second.EntriesMigrated)
	}
}

// TestMigrateSubBucketWarning: when nested sub-buckets exist, the
// migration must surface a warning and prefix the migrated keys.
func TestMigrateSubBucketWarning(t *testing.T) {
	dir := t.TempDir()
	seedBolt(t, filepath.Join(dir, HTTPCacheBoltFile),
		map[string]map[string]string{"top": {"a": "1"}},
		map[string]map[string]map[string]string{
			"top": {"nested": {"k": "v"}},
		})
	res, err := Migrate(MigrateOptions{CacheDir: dir})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.SubBucketsFound == 0 {
		t.Error("SubBucketsFound=0; want >0")
	}
	if len(res.Warnings) == 0 {
		t.Error("Warnings empty; spec mandates sub-bucket warning")
	}
	// Verify the nested entry was migrated under the prefixed key.
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	defer func() { _ = s.Close() }()
	got, ok, _ := s.Get("top/nested/k")
	if !ok || string(got) != "v" {
		t.Errorf("Get(top/nested/k)=(%q,%v); want (v,true)", got, ok)
	}
}
