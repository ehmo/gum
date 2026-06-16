// Spec §10.2 acceptance for the WAL-SQLite HTTP cache. Validates the
// pragma bake-in, kv round-trip semantics, TTL expiry, sentinel
// read/write helpers, and the ErrSQLiteCorrupt path for non-database
// files. Migration sequencing lives in migrate_test.go.

package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTempSQLite returns an open SQLiteWALCache rooted in a fresh tempdir.
// Tests close the handle but leave the tempdir to t.Cleanup.
func newTempSQLite(t *testing.T) *SQLiteWALCache {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: filepath.Join(dir, HTTPWALDBFile)})
	if err != nil {
		t.Fatalf("OpenSQLiteWAL: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestOpenSQLiteWALEmptyPath asserts the Path-required guard.
func TestOpenSQLiteWALEmptyPath(t *testing.T) {
	if _, err := OpenSQLiteWAL(SQLiteConfig{}); err == nil {
		t.Fatal("OpenSQLiteWAL with empty Path returned nil error; want guard")
	}
}

// TestOpenSQLiteWALCreatesFileAndKVTable confirms a fresh open materializes
// the file under MkdirAll-created parents and creates the kv table so
// callers can immediately Set/Get without an explicit migration call.
func TestOpenSQLiteWALCreatesFileAndKVTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "sub", HTTPWALDBFile)
	s, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err != nil {
		t.Fatalf("OpenSQLiteWAL: %v", err)
	}
	defer func() { _ = s.Close() }()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("sqlite file not created: %v", err)
	}
	if s.Path() != path {
		t.Errorf("Path()=%q; want %q", s.Path(), path)
	}
	// Immediate Set proves kv table exists.
	if err := s.Set("k", []byte("v"), 0); err != nil {
		t.Fatalf("Set on fresh db: %v", err)
	}
}

// TestSQLiteSetGetRoundTrip pins the basic key→value semantics.
func TestSQLiteSetGetRoundTrip(t *testing.T) {
	s := newTempSQLite(t)
	if err := s.Set("alpha", []byte("beta"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok, err := s.Get("alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("Get returned ok=false for present key")
	}
	if string(got) != "beta" {
		t.Errorf("Get value=%q; want beta", got)
	}
}

// TestSQLiteGetMiss confirms absent keys return (nil, false, nil).
func TestSQLiteGetMiss(t *testing.T) {
	s := newTempSQLite(t)
	v, ok, err := s.Get("absent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok || v != nil {
		t.Errorf("Get(absent) = (%v, %v); want (nil, false)", v, ok)
	}
}

// TestSQLiteSetReplaces confirms INSERT OR REPLACE behavior.
func TestSQLiteSetReplaces(t *testing.T) {
	s := newTempSQLite(t)
	_ = s.Set("k", []byte("v1"), 0)
	_ = s.Set("k", []byte("v2"), 0)
	got, ok, _ := s.Get("k")
	if !ok || string(got) != "v2" {
		t.Errorf("Set replacement: got=%q ok=%v; want v2 true", got, ok)
	}
}

// TestSQLiteTTLExpiry confirms the expires_at_unix gate.
func TestSQLiteTTLExpiry(t *testing.T) {
	s := newTempSQLite(t)
	if err := s.Set("short", []byte("ephemeral"), time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	_, ok, err := s.Get("short")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Error("TTL-expired key returned ok=true; want false")
	}
}

// TestSQLiteSentinelLifecycle covers the SentinelPresent + WriteSentinel
// pair used by Migrate to decide which branch to take.
func TestSQLiteSentinelLifecycle(t *testing.T) {
	s := newTempSQLite(t)
	ok, err := s.SentinelPresent()
	if err != nil {
		t.Fatalf("SentinelPresent (pre): %v", err)
	}
	if ok {
		t.Fatal("fresh db reports sentinel present; want false")
	}
	if err := s.WriteSentinel(); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	ok, err = s.SentinelPresent()
	if err != nil {
		t.Fatalf("SentinelPresent (post): %v", err)
	}
	if !ok {
		t.Error("SentinelPresent after write = false; want true")
	}
	// Sentinel value should equal SentinelValue constant.
	got, present, err := s.Get(SentinelKey)
	if err != nil || !present {
		t.Fatalf("Get(SentinelKey) present=%v err=%v", present, err)
	}
	if string(got) != SentinelValue {
		t.Errorf("sentinel value=%q; want %q", got, SentinelValue)
	}
}

// TestSQLiteOpenCorruptFile feeds a garbage payload into a file and
// confirms ErrSQLiteCorrupt fires. The driver flags the violation on
// Ping (not Open), which the OpenSQLiteWAL wraps consistently.
func TestSQLiteOpenCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HTTPWALDBFile)
	if err := os.WriteFile(path, []byte("not a sqlite database — just bytes"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	_, err := OpenSQLiteWAL(SQLiteConfig{Path: path})
	if err == nil {
		t.Fatal("OpenSQLiteWAL on corrupt file returned nil; want ErrSQLiteCorrupt")
	}
	if !errors.Is(err, ErrSQLiteCorrupt) {
		t.Errorf("err = %v; want errors.Is ErrSQLiteCorrupt", err)
	}
}

// TestSQLiteJournalModeIsWAL confirms the DSN pragma actually took effect
// (spec §10.2 requires WAL for concurrent reader/writer access).
func TestSQLiteJournalModeIsWAL(t *testing.T) {
	s := newTempSQLite(t)
	// Force a write so the WAL file actually materializes.
	if err := s.Set("warmup", []byte("x"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	row := s.db.QueryRow("PRAGMA journal_mode")
	var mode string
	if err := row.Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode=%q; want wal", mode)
	}
}

// TestSQLiteCloseIdempotent confirms double-close doesn't panic.
func TestSQLiteCloseIdempotent(t *testing.T) {
	s := newTempSQLite(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}
	// Second close on the same handle returns a database/sql error but
	// must not panic; we tolerate either nil or a non-nil error.
	_ = s.Close()
	var nilS *SQLiteWALCache
	if err := nilS.Close(); err != nil {
		t.Errorf("nil-receiver Close: %v; want nil", err)
	}
}
