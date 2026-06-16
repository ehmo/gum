package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOpenSQLiteWALMkdirAllErrorWraps pins OpenSQLiteWAL's
// `os.MkdirAll err → wrap` arm (sqlite_wal.go:72-74). Reached by
// planting a regular file at the parent dir so MkdirAll(.../sub)
// fails with ENOTDIR.
func TestOpenSQLiteWALMkdirAllErrorWraps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	_, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(blocker, "sub", "http-wal.db")})
	if err == nil {
		t.Fatal("OpenSQLiteWAL(blocked mkdir) err=nil; want mkdir wrap")
	}
	if !strings.Contains(err.Error(), "create cache dir") {
		t.Errorf("err=%q; want 'create cache dir' wrap", err.Error())
	}
}

// TestSQLiteWALSetAfterCloseWraps pins Set's `db.Exec err → wrap` arm
// (sqlite_wal.go:134-136). Reached by Close()-ing the cache and then
// calling Set — database/sql returns "sql: database is closed".
func TestSQLiteWALSetAfterCloseWraps(t *testing.T) {
	t.Parallel()
	s := newTempSQLite(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	err := s.Set("k", []byte("v"), time.Minute)
	if err == nil {
		t.Fatal("Set(after close) err=nil; want sqlite set wrap")
	}
	if !strings.Contains(err.Error(), "sqlite set") {
		t.Errorf("err=%q; want 'sqlite set' wrap", err.Error())
	}
}

// TestSQLiteWALGetAfterCloseWraps pins Get's `row.Scan err (non-NoRows)
// → wrap` arm (sqlite_wal.go:151). After Close, Scan returns "sql:
// database is closed" — distinct from sql.ErrNoRows, so the wrap fires.
func TestSQLiteWALGetAfterCloseWraps(t *testing.T) {
	t.Parallel()
	s := newTempSQLite(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, _, err := s.Get("k")
	if err == nil {
		t.Fatal("Get(after close) err=nil; want sqlite get wrap")
	}
	if !strings.Contains(err.Error(), "sqlite get") {
		t.Errorf("err=%q; want 'sqlite get' wrap", err.Error())
	}
}

// TestSQLiteWALSentinelPresentAfterCloseWraps pins SentinelPresent's
// `row.Scan err (non-NoRows) → wrap` arm (sqlite_wal.go:170). Same
// closed-DB technique as above.
func TestSQLiteWALSentinelPresentAfterCloseWraps(t *testing.T) {
	t.Parallel()
	s := newTempSQLite(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := s.SentinelPresent()
	if err == nil {
		t.Fatal("SentinelPresent(after close) err=nil; want sentinel check wrap")
	}
	if !strings.Contains(err.Error(), "sentinel check") {
		t.Errorf("err=%q; want 'sentinel check' wrap", err.Error())
	}
}
