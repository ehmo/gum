package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateEmptyCacheDirRejected pins Migrate's
// `opts.CacheDir == "" → error` arm (migrate.go:85-87). The required-field
// guard MUST surface a typed error rather than dereferencing a zero path.
func TestMigrateEmptyCacheDirRejected(t *testing.T) {
	t.Parallel()
	_, err := Migrate(MigrateOptions{})
	if err == nil {
		t.Fatal("Migrate(empty CacheDir) err=nil; want required-field error")
	}
	if !strings.Contains(err.Error(), "CacheDir is required") {
		t.Errorf("err=%q; want 'CacheDir is required'", err.Error())
	}
}

// TestMigrateBranch4OpenWALErrorPropagates pins Migrate's branch-4
// `OpenSQLiteWAL err → return err` arm (migrate.go:138-140). Reached by
// planting a regular file as the would-be CacheDir parent so MkdirAll
// inside OpenSQLiteWAL trips ENOTDIR.
func TestMigrateBranch4OpenWALErrorPropagates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	cacheDir := filepath.Join(blocker, "sub", "cache")
	_, err := Migrate(MigrateOptions{CacheDir: cacheDir})
	if err == nil {
		t.Fatal("Migrate(blocker parent) err=nil; want ENOTDIR-wrapped err")
	}
}
