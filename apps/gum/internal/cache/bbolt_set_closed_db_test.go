package cache_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
)

// TestBBoltSetAfterCloseWrapsBoltWriteError pins Set's
// `c.db.Update err → return fmt.Errorf("cache: bbolt write: %w", ...)`
// arm (bbolt.go:229-231). Once Close has run, bbolt's Update returns
// "database not open" and Set MUST wrap it so the caller sees the
// "cache: bbolt write:" prefix rather than a bare bbolt error.
func TestBBoltSetAfterCloseWrapsBoltWriteError(t *testing.T) {
	t.Parallel()
	c, err := cache.Open(cache.BBoltConfig{Path: filepath.Join(t.TempDir(), "cache.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err = c.Set("k", []byte("v"), time.Minute)
	if err == nil {
		t.Fatal("Set(closed-db) err=nil; want bbolt write wrap")
	}
	if !strings.Contains(err.Error(), "cache: bbolt write:") {
		t.Errorf("err=%q; want 'cache: bbolt write:' prefix", err.Error())
	}
}
