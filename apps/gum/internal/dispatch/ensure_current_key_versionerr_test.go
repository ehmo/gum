package dispatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEnsureCurrentKeyVersionErrorPropagates pins ensureCurrentKey's
// `currentKeyVersion err → return err` arm (confirmation.go:185-187).
// Reached by planting a readable confirmation.key inside a keyDir that
// then has its read bit stripped — MkdirAll on the existing dir is a
// no-op, ReadFile of the known key path succeeds (only x is needed to
// walk in), but ReadDir trips EACCES so currentKeyVersion surfaces it.
func TestEnsureCurrentKeyVersionErrorPropagates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix mode bits")
	}
	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "confirmation.key")
	if err := os.WriteFile(keyPath, make([]byte, 32), 0o600); err != nil {
		t.Fatalf("plant key: %v", err)
	}
	// 0o300 = wx (no r) so ReadDir fails while ReadFile of a known path
	// still works (lookup needs x, not r). Restore in cleanup so TempDir
	// removal succeeds.
	if err := os.Chmod(keyDir, 0o300); err != nil {
		t.Fatalf("chmod keyDir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(keyDir, 0o700) })

	s, err := NewTokenStore(8, time.Minute, keyDir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	if _, _, err := s.ensureCurrentKey(); err == nil {
		t.Fatal("ensureCurrentKey(no-r keyDir)=nil err; want currentKeyVersion ReadDir err")
	}
}

// TestEnsureCurrentKeyWriteFileErrorPropagates pins the
// `WriteFile keyPath err → "write key file:" wrap` arm
// (confirmation.go:176-178). Reached by chmod'ing the keyDir to r+x
// only (no w) so ReadFile returns ENOENT (triggering key-gen), then
// WriteFile of the new key fails with EACCES.
func TestEnsureCurrentKeyWriteFileErrorPropagates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix mode bits")
	}
	keyDir := t.TempDir()
	if err := os.Chmod(keyDir, 0o500); err != nil {
		t.Fatalf("chmod keyDir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(keyDir, 0o700) })

	s, err := NewTokenStore(8, time.Minute, keyDir)
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	if _, _, err := s.ensureCurrentKey(); err == nil {
		t.Fatal("ensureCurrentKey(no-w keyDir)=nil err; want write-key-file wrap")
	}
}
