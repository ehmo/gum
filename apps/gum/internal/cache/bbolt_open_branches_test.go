package cache_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cache"
)

// TestBBoltOpenUsesDefaultPathFromHome pins the cfg.Path=="" branch:
// callers (notably init) rely on the cache landing under
// $HOME/.cache/gum/cache.db so the cleanup tooling can find it.
func TestBBoltOpenUsesDefaultPathFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	c, err := cache.Open(cache.BBoltConfig{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	want := filepath.Join(home, ".cache", "gum", "cache.db")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected default cache db at %q; stat err=%v", want, err)
	}
}

// TestBBoltOpenMkdirAllFailureWraps pins the MkdirAll error branch.
// Planting a regular file at the would-be parent directory location
// causes MkdirAll to fail with "not a directory", which Open must
// surface as a "create cache dir" error rather than panicking.
func TestBBoltOpenMkdirAllFailureWraps(t *testing.T) {
	dir := t.TempDir()
	// Make 'dir/cache-blocker' a regular file so MkdirAll("dir/cache-blocker/")
	// fails with ENOTDIR.
	blocker := filepath.Join(dir, "cache-blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(blocker, "nested", "cache.db")
	_, err := cache.Open(cache.BBoltConfig{Path: dbPath})
	if err == nil {
		t.Fatalf("expected mkdir error; got nil")
	}
	if !strings.Contains(err.Error(), "create cache dir") {
		t.Errorf("err=%v; want 'create cache dir' wrap", err)
	}
}
