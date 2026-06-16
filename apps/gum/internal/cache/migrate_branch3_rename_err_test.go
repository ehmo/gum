package cache_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/ehmo/gum/internal/cache"
)

// TestMigrateBranch3RenameBoltToBakErrorWraps pins Migrate's branch-3
// `os.Rename(boltPath, bakPath) err → "cache: rename bolt to bak:"`
// arm (migrate.go:162-164). When the migration copies BoltDB rows
// into a fresh WAL successfully but the final bolt→bak rename trips
// (here: bakPath pre-planted as a non-empty directory → EISDIR/
// ENOTEMPTY), Migrate MUST surface the wrap so operators see exactly
// which step failed — the WAL is already populated and sentinel'd,
// so retry semantics differ from a bolt-read or sentinel-write
// failure.
func TestMigrateBranch3RenameBoltToBakErrorWraps(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can rename over a populated dir on some kernels")
	}
	t.Parallel()
	dir := t.TempDir()

	// Seed a real BoltDB with one key so copyBoltToSQLite succeeds and
	// reaches the rename step.
	boltPath := filepath.Join(dir, cache.HTTPCacheBoltFile)
	db, err := bolt.Open(boltPath, 0o600, nil)
	if err != nil {
		t.Fatalf("seed bolt: %v", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("kv"))
		if err != nil {
			return err
		}
		return b.Put([]byte("k"), []byte("v"))
	}); err != nil {
		_ = db.Close()
		t.Fatalf("seed bucket: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close bolt: %v", err)
	}

	// Plant bakPath as a non-empty directory so os.Rename returns
	// EISDIR/ENOTEMPTY.
	bakPath := filepath.Join(dir, cache.HTTPCacheBoltBakFile)
	if err := os.MkdirAll(bakPath, 0o700); err != nil {
		t.Fatalf("mkdir bak: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bakPath, "occupant"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed bak occupant: %v", err)
	}

	_, err = cache.Migrate(cache.MigrateOptions{CacheDir: dir})
	if err == nil {
		t.Fatal("Migrate(branch3 with dir-shaped bak) err=nil; want rename-bolt-to-bak failure")
	}
	if !strings.Contains(err.Error(), "rename bolt to bak:") {
		t.Errorf("err=%q; want 'rename bolt to bak:' prefix", err.Error())
	}
}
