package cache_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cache"
)

// TestMigrateBranch1RenameLeftoverBoltErrorWraps pins Migrate's branch-1
// `os.Rename(boltPath, bakPath) err → return "cache: rename leftover bolt:"`
// arm (migrate.go:115-117). When wal+sentinel are in place AND a stale bolt
// is present, Migrate renames bolt → bak to converge on subsequent runs. We
// pre-create bakPath as a non-empty directory so the rename returns EISDIR
// /ENOTEMPTY, and assert the error carries the "rename leftover bolt:"
// prefix rather than a bare syscall message.
func TestMigrateBranch1RenameLeftoverBoltErrorWraps(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can rename over a populated dir on some kernels")
	}
	t.Parallel()
	dir := t.TempDir()

	// Seed wal + sentinel.
	s, err := cache.OpenSQLiteWAL(cache.SQLiteConfig{Path: filepath.Join(dir, cache.HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("OpenSQLiteWAL seed: %v", err)
	}
	if err := s.WriteSentinel(); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Seed a leftover bolt file (just needs to exist for branch 1's rename).
	boltPath := filepath.Join(dir, cache.HTTPCacheBoltFile)
	if err := os.WriteFile(boltPath, []byte("stale bolt"), 0o600); err != nil {
		t.Fatalf("seed bolt: %v", err)
	}

	// Plant bakPath as a directory with content so rename fails.
	bakPath := filepath.Join(dir, cache.HTTPCacheBoltBakFile)
	if err := os.MkdirAll(bakPath, 0o700); err != nil {
		t.Fatalf("mkdir bak: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bakPath, "occupant"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed bak occupant: %v", err)
	}

	_, err = cache.Migrate(cache.MigrateOptions{CacheDir: dir})
	if err == nil {
		t.Fatal("Migrate(branch1 with dir-shaped bak) err=nil; want rename failure")
	}
	if !strings.Contains(err.Error(), "rename leftover bolt:") {
		t.Errorf("err=%q; want 'rename leftover bolt:' prefix", err.Error())
	}
}

// TestMigrateBranch3CopyBoltErrorPropagates pins Migrate's branch-3
// `copyBoltToSQLite err → _ = os.Remove(walPath); return nil, err` arm
// (migrate.go:151-156). With no wal but an unreadable bolt file
// (chmod 0o000), the bolt.Open inside copyBoltToSQLite fails EACCES; the
// outer Migrate cleans up any partial wal and surfaces the wrapped err.
// Skipped on root since root bypasses mode bits.
func TestMigrateBranch3CopyBoltErrorPropagates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix mode bits")
	}
	t.Parallel()
	dir := t.TempDir()
	boltPath := filepath.Join(dir, cache.HTTPCacheBoltFile)
	if err := os.WriteFile(boltPath, []byte("ignored"), 0o000); err != nil {
		t.Fatalf("seed unreadable bolt: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(boltPath, 0o600) })

	_, err := cache.Migrate(cache.MigrateOptions{CacheDir: dir})
	if err == nil {
		t.Fatal("Migrate(unreadable bolt) err=nil; want bolt.Open EACCES propagation")
	}
	if !strings.Contains(err.Error(), "open bolt for migration:") {
		t.Errorf("err=%q; want 'open bolt for migration:' prefix", err.Error())
	}
}
