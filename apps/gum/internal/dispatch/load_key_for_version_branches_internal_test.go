package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadKeyForVersionMissingFileSurfacesError pins the
// `os.ReadFile err → return nil, err` arm. Asking for an older
// key-version whose backing confirmation.key.<N> file is absent
// MUST surface the err — VerifyConfirmationToken depends on this so
// it can fall through to "token signed with an unknown key
// version" → reject. Silently treating absence as a usable key would
// open an avenue to forge a token under a rotated-away key.
func TestLoadKeyForVersionMissingFileSurfacesError(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewTokenStore(8, time.Minute, tmp)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	// currentKeyVersion will be 1 (no rotated keys present). Asking
	// for version 99 routes to a confirmation.key.<suffix> path that
	// doesn't exist on disk → ReadFile ENOENT.
	if _, err := s.loadKeyForVersion(99); err == nil {
		t.Fatal("loadKeyForVersion(99)=nil err; want ENOENT surface")
	}
}

// TestLoadKeyForVersionWrongLengthSurfacesError pins the
// `len(data) != 32 → return wrapped err` arm. A corrupted key file
// (e.g. truncated during a crash) MUST be rejected rather than used
// as if it were valid — the HMAC would still compute over whatever
// bytes are present but with no integrity guarantee, opening a path
// to silently accepting tampered tokens.
func TestLoadKeyForVersionWrongLengthSurfacesError(t *testing.T) {
	tmp := t.TempDir()
	// Plant a truncated current key (16 bytes instead of 32).
	keyPath := filepath.Join(tmp, "confirmation.key")
	if err := os.WriteFile(keyPath, make([]byte, 16), 0o600); err != nil {
		t.Fatalf("plant truncated key: %v", err)
	}
	s, err := NewTokenStore(8, time.Minute, tmp)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}

	// currentKeyVersion still resolves to 1 (no rotated files).
	// loadKeyForVersion(1) reads the current key path → 16 bytes →
	// wrong-length guard fires.
	_, err = s.loadKeyForVersion(1)
	if err == nil {
		t.Fatal("loadKeyForVersion(truncated)=nil err; want wrong-length surface")
	}
	if !strings.Contains(err.Error(), "wrong length 16") {
		t.Errorf("err=%v; want 'wrong length 16' wrap", err)
	}
}
