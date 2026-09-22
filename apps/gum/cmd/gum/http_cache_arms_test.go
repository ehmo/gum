package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cache"
	profilepkg "github.com/ehmo/gum/internal/profile"
)

// holdHTTPCache opens the profile's §10.2 store and keeps it open, which is
// what a running MCP server looks like to a second gum process: bbolt takes an
// exclusive file lock.
func holdHTTPCache(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	c, err := cache.Open(cache.BBoltConfig{Path: filepath.Join(dir, cache.HTTPCacheBoltFile)})
	if err != nil {
		t.Fatalf("hold cache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
}

// An absent store is not an error. Nothing was cached, so a clear has nothing
// to do and stats have nothing to report.
func TestHTTPCacheHelpersOnAnAbsentStore(t *testing.T) {
	dir := t.TempDir()

	n, err := clearHTTPCacheDir(dir, "")
	if err != nil || n != 0 {
		t.Errorf("clearHTTPCacheDir = %d, %v; want 0, nil", n, err)
	}
	if u := measureHTTPCacheDir(dir); u != (cache.HTTPUsage{}) {
		t.Errorf("measureHTTPCacheDir = %+v; want the zero value", u)
	}
}

// A store another process holds must produce a named error rather than a
// silent zero: deleting rows under a live process would leave it serving a
// hot tier the disk no longer backs.
func TestClearHTTPCacheDirReportsALockedStore(t *testing.T) {
	dir := t.TempDir()
	holdHTTPCache(t, dir)

	_, err := clearHTTPCacheDir(dir, "")
	if err == nil {
		t.Fatal("clearing a locked store must fail")
	}
	if !strings.Contains(err.Error(), "in use by another gum process") {
		t.Errorf("err = %v; want the in-use message", err)
	}
}

// Stats must never block behind another process's lock. A store it cannot
// open reports zero, which is what this process truthfully knows.
func TestMeasureHTTPCacheDirOnALockedStore(t *testing.T) {
	dir := t.TempDir()
	holdHTTPCache(t, dir)

	if u := measureHTTPCacheDir(dir); u != (cache.HTTPUsage{}) {
		t.Errorf("measureHTTPCacheDir = %+v; want the zero value on a locked store", u)
	}
}

// A file that is not a bbolt database fails both helpers the same way an
// absent one does for stats, and surfaces the open error for a clear.
func TestHTTPCacheHelpersOnACorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cache.HTTPCacheBoltFile), []byte("not a bolt file"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := clearHTTPCacheDir(dir, ""); err == nil {
		t.Error("clearing an unreadable store must fail")
	}
	if u := measureHTTPCacheDir(dir); u != (cache.HTTPUsage{}) {
		t.Errorf("measureHTTPCacheDir = %+v; want the zero value", u)
	}
}

// openHTTPCacheStore never fails a dispatch. Every unavailable path returns
// two nils, which disables conditional requests and nothing else.
func TestOpenHTTPCacheStoreDegradesToNil(t *testing.T) {
	t.Run("an unresolved profile name", func(t *testing.T) {
		store, closer := openHTTPCacheStore("default", errors.New("no profile"))
		if store != nil || closer != nil {
			t.Errorf("store=%v closer!=nil=%v; want nil, nil", store, closer != nil)
		}
	})

	t.Run("a locked store", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", root)
		holdHTTPCache(t, filepath.Join(root, "gum", "default"))

		store, closer := openHTTPCacheStore(profilepkg.Name("default"), nil)
		if store != nil || closer != nil {
			t.Errorf("store=%v closer!=nil=%v; want nil, nil", store, closer != nil)
		}
	})

	t.Run("a corrupt store", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", root)
		dir := filepath.Join(root, "gum", "default")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, cache.HTTPCacheBoltFile), []byte("not a bolt file"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}

		store, closer := openHTTPCacheStore(profilepkg.Name("default"), nil)
		if store != nil || closer != nil {
			t.Errorf("store=%v closer!=nil=%v; want nil, nil", store, closer != nil)
		}
	})

	t.Run("a usable store", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())

		store, closer := openHTTPCacheStore(profilepkg.Name("default"), nil)
		if store == nil || closer == nil {
			t.Fatalf("store=%v closer!=nil=%v; want a store and a closer", store, closer != nil)
		}
		if err := closer(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
}

// `gum cache clear` must surface a locked store as a failed command, not as a
// success that cleared nothing.
func TestCacheClearFailsOnALockedStore(t *testing.T) {
	root := withTempCacheRootCLI(t)
	holdHTTPCache(t, profileCacheDir(root))

	out, err := runCLI(t, "cache", "clear")
	if err == nil {
		t.Fatalf("gum cache clear on a locked store must fail; stdout=%q", out)
	}
	if !strings.Contains(err.Error(), "in use by another gum process") {
		t.Errorf("err = %v; want the in-use message", err)
	}
}
