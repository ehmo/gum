package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEnsureCurrentKeyRejectsWrongLength pins the malformed-key-file
// guard: a confirmation.key on disk with anything other than 32 bytes
// must error rather than silently producing weaker HMAC keys. The
// guard exists because a partial write (truncated key) would otherwise
// be loaded verbatim and downgrade token integrity.
func TestEnsureCurrentKeyRejectsWrongLength(t *testing.T) {
	keyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(keyDir, "confirmation.key"), []byte("short"), 0o600); err != nil {
		t.Fatalf("plant short key: %v", err)
	}
	s, err := NewTokenStore(8, time.Minute, keyDir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	_, _, err = s.ensureCurrentKey()
	if err == nil {
		t.Fatalf("expected wrong-length error")
	}
	if !strings.Contains(err.Error(), "wrong length") {
		t.Errorf("err=%q; want wrong-length message", err)
	}
}

// TestEnsureCurrentKeyMkdirAllError pins the directory-create failure
// branch: a regular file at the keyDir path makes MkdirAll fail, and
// the wrap must include "create key dir" so operators can distinguish
// it from the later read/write failure paths.
func TestEnsureCurrentKeyMkdirAllError(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	keyDir := filepath.Join(blocker, "subdir")
	s, err := NewTokenStore(8, time.Minute, keyDir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	_, _, err = s.ensureCurrentKey()
	if err == nil {
		t.Fatalf("expected MkdirAll error")
	}
	if !strings.Contains(err.Error(), "create key dir") {
		t.Errorf("err=%q; want create-key-dir wrap", err)
	}
}

// TestEnsureCurrentKeyGeneratesOnFirstCall is the happy path for a
// fresh dir: the first call creates a 32-byte key file with 0o600
// permissions, subsequent calls round-trip the same bytes back.
func TestEnsureCurrentKeyGeneratesOnFirstCall(t *testing.T) {
	keyDir := t.TempDir()
	s, err := NewTokenStore(8, time.Minute, keyDir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	k1, v1, err := s.ensureCurrentKey()
	if err != nil {
		t.Fatalf("first ensureCurrentKey: %v", err)
	}
	if len(k1) != 32 {
		t.Errorf("key len=%d; want 32", len(k1))
	}
	if v1 <= 0 {
		t.Errorf("version=%d; want >0", v1)
	}

	k2, v2, err := s.ensureCurrentKey()
	if err != nil {
		t.Fatalf("second ensureCurrentKey: %v", err)
	}
	if string(k1) != string(k2) {
		t.Errorf("second call produced different key bytes")
	}
	if v1 != v2 {
		t.Errorf("version changed across calls: %d -> %d", v1, v2)
	}
}
