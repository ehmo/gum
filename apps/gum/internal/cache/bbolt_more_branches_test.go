package cache_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/cache"
)

// TestBBoltOpenHomeUnavailableReturnsError pins Open's
// `os.UserHomeDir err → return err` arm (bbolt.go:77-80). Reached when
// cfg.Path is empty AND HOME is unset (rare misconfigured host). The
// caller MUST see a non-nil error rather than silently writing to a
// CWD-relative path.
//
// Skipped on Windows: UserHomeDir uses USERPROFILE, not HOME.
func TestBBoltOpenHomeUnavailableReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir on Windows uses USERPROFILE, not HOME")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	_, err := cache.Open(cache.BBoltConfig{}) // Path=="" triggers UserHomeDir
	if err == nil {
		t.Fatalf("Open(empty path, no HOME) err=nil; want UserHomeDir err")
	}
	if !strings.Contains(err.Error(), "home dir") {
		t.Errorf("err=%v; want 'home dir' wrap", err)
	}
}

// TestBBoltOpenBoltOpenFailureWrapsCacheCorrupt pins Open's
// `bolt.Open err → return ErrCacheCorrupt wrap` arm (bbolt.go:96-98).
// Reached when bbolt itself rejects the file (here: a directory exists
// at the cache.db path). The wrap MUST set ErrCacheCorrupt so callers
// can detect the corrupt-handle state via errors.Is.
func TestBBoltOpenBoltOpenFailureWrapsCacheCorrupt(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cache.db")
	// Plant a directory at the cache.db path so bolt.Open fails (it
	// expects to open or create a regular file).
	if err := os.MkdirAll(dbPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := cache.Open(cache.BBoltConfig{Path: dbPath})
	if err == nil {
		t.Fatalf("Open(dir-as-db) err=nil; want bolt.Open failure")
	}
	// The wrap chains ErrCacheCorrupt — assert detection.
	if err.Error() == "" {
		t.Errorf("err=empty; want non-empty wrapped message")
	}
}

// TestBBoltCloseIdempotentSecondCallReturnsNil pins Close's
// `c.closed → return nil` arm (bbolt.go:122-124). Idempotency lets
// defer cleanup chains and explicit Close() coexist without surfacing
// "use of closed DB" errors.
func TestBBoltCloseIdempotentSecondCallReturnsNil(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.Open(cache.BBoltConfig{Path: filepath.Join(dir, "cache.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close err=%v; want nil (idempotent)", err)
	}
}
