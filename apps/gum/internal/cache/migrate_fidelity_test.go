// Spec §10.2 step 3 fidelity: the migration copies rows inside one SQLite
// transaction, keeps the cache bucket's keys reachable, and carries each
// entry's expiry across. Also covers the --force escape hatch for a corrupt
// http-wal.db (beads gum-skxi, gum-xar1).

package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// seedBoltRecords writes cacheRecord-shaped values into the gum-cache bucket,
// which is the shape BBoltCache writes and therefore the shape a real
// http.db holds.
func seedBoltRecords(t *testing.T, path string, records map[string]cacheRecord) {
	t.Helper()
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open bolt: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(cacheBucket)
		if err != nil {
			return err
		}
		for k, rec := range records {
			data, err := json.Marshal(rec)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(k), data); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed bolt: %v", err)
	}
}

// TestMigrateKeepsCacheBucketKeysBare pins gum-skxi's key rewrite. The
// migration prefixed every key with its bucket name, so every entry
// BBoltCache had written under a bare key became unreachable in the new
// store.
func TestMigrateKeepsCacheBucketKeysBare(t *testing.T) {
	dir := t.TempDir()
	seedBoltRecords(t, filepath.Join(dir, HTTPCacheBoltFile), map[string]cacheRecord{
		"op:args:subject": {Payload: []byte("body"), Size: 4, LastAccessUnix: 1},
	})

	if _, err := Migrate(MigrateOptions{CacheDir: dir}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	s, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	defer func() { _ = s.Close() }()

	got, ok, err := s.Get("op:args:subject")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("migrated entry is unreachable under its own key")
	}
	if string(got) != "body" {
		t.Errorf("Get = %q; want the record's payload %q", got, "body")
	}
}

// TestMigratePreservesEntryExpiry pins the other half of gum-skxi: every row
// was written with ttl 0, so an entry that had already expired came back to
// life in the new store and a live entry lost its deadline.
func TestMigratePreservesEntryExpiry(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Unix()
	seedBoltRecords(t, filepath.Join(dir, HTTPCacheBoltFile), map[string]cacheRecord{
		"live": {Payload: []byte("l"), ExpiresAtUnix: now + 3600, Size: 1, LastAccessUnix: now},
		"dead": {Payload: []byte("d"), ExpiresAtUnix: now - 3600, Size: 1, LastAccessUnix: now},
	})

	if _, err := Migrate(MigrateOptions{CacheDir: dir}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	s, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, ok, _ := s.Get("live"); !ok {
		t.Error("live entry is a miss after migration")
	}
	if _, ok, _ := s.Get("dead"); ok {
		t.Error("expired entry is a hit after migration; its expiry was dropped")
	}
}

// TestImportIsInvisibleUntilCommit pins spec §10.2 step 3's single
// transaction. Row-by-row autocommit let another process read half a
// migration.
func TestImportIsInvisibleUntilCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), HTTPWALDBFile)
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	defer func() { _ = s.Close() }()

	reader, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	im, err := s.BeginImport()
	if err != nil {
		t.Fatalf("BeginImport: %v", err)
	}
	if err := im.Add("half", []byte("written"), 0); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if _, ok, _ := reader.Get("half"); ok {
		t.Error("a second connection read a row from an uncommitted import")
	}
	if ok, _ := reader.SentinelPresent(); ok {
		t.Error("sentinel visible before commit")
	}

	if err := im.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, ok, _ := reader.Get("half"); !ok {
		t.Error("row absent after commit")
	}
	if ok, _ := reader.SentinelPresent(); !ok {
		t.Error("Commit did not write the sentinel as its final statement")
	}
}

// TestImportRollbackLeavesNothing asserts an abandoned import writes no row
// and no sentinel, which is what branch 2 relies on to detect a crash.
func TestImportRollbackLeavesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), HTTPWALDBFile)
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("open wal: %v", err)
	}
	defer func() { _ = s.Close() }()

	im, err := s.BeginImport()
	if err != nil {
		t.Fatalf("BeginImport: %v", err)
	}
	if err := im.Add("k", []byte("v"), 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := im.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if _, ok, _ := s.Get("k"); ok {
		t.Error("row survived a rolled-back import")
	}
	if ok, _ := s.SentinelPresent(); ok {
		t.Error("sentinel survived a rolled-back import")
	}
}

// TestMigrateForceRecoversCorruptWAL pins gum-xar1. sqlite_wal.go documents
// `gum cache migrate --force` as the recovery path for ErrSQLiteCorrupt, but
// Migrate opened the file before it read opts.Force, so the documented escape
// hatch returned the very error it was meant to clear.
func TestMigrateForceRecoversCorruptWAL(t *testing.T) {
	dir := t.TempDir()
	walPath := filepath.Join(dir, HTTPWALDBFile)
	if err := os.WriteFile(walPath, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write corrupt wal: %v", err)
	}
	seedBoltRecords(t, filepath.Join(dir, HTTPCacheBoltFile), map[string]cacheRecord{
		"survivor": {Payload: []byte("p"), Size: 1, LastAccessUnix: 1},
	})

	res, err := Migrate(MigrateOptions{CacheDir: dir, Force: true})
	if err != nil {
		t.Fatalf("Migrate --force over a corrupt wal: %v", err)
	}
	if !res.SentinelWritten {
		t.Error("SentinelWritten=false after a forced re-migration")
	}
	if res.EntriesMigrated != 1 {
		t.Errorf("EntriesMigrated=%d; want 1", res.EntriesMigrated)
	}

	s, err := OpenSQLiteWAL(SQLiteConfig{Path: walPath})
	if err != nil {
		t.Fatalf("open rebuilt wal: %v", err)
	}
	defer func() { _ = s.Close() }()
	if _, ok, _ := s.Get("survivor"); !ok {
		t.Error("rebuilt wal is missing the migrated entry")
	}
}

// TestMigrateCorruptWALWithoutForceStillFails keeps the guard: without
// --force a corrupt file is an operator decision, not something the tool
// silently deletes.
func TestMigrateCorruptWALWithoutForceStillFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, HTTPWALDBFile), []byte("not sqlite"), 0o600); err != nil {
		t.Fatalf("write corrupt wal: %v", err)
	}
	if _, err := Migrate(MigrateOptions{CacheDir: dir}); !errors.Is(err, ErrSQLiteCorrupt) {
		t.Fatalf("Migrate over a corrupt wal = %v; want ErrSQLiteCorrupt", err)
	}
}
