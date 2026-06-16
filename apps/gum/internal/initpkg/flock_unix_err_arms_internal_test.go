//go:build !windows

package initpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAcquireSettingsLockMkdirErrorWrapsWithMkdir pins acquireSettingsLock's
// `MkdirAll err → "mkdir for lock %s" wrap` arm (flock_unix.go:18-20).
// Reached when a regular file sits at the parent-dir path so MkdirAll
// trips with ENOTDIR. The wrap names "mkdir for lock" so operators
// distinguish dir-creation failures from flock failures further down.
func TestAcquireSettingsLockMkdirErrorWrapsWithMkdir(t *testing.T) {
	base := t.TempDir()
	// Plant a regular file at the parent path so MkdirAll fails with ENOTDIR.
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	lockPath := filepath.Join(blocker, "nested", "settings.lock")

	_, err := acquireSettingsLock(lockPath, 100*time.Millisecond)
	if err == nil {
		t.Fatalf("acquireSettingsLock(blocked parent) err=nil; want MkdirAll err")
	}
	if !strings.Contains(err.Error(), "mkdir for lock") {
		t.Errorf("err=%v; want 'mkdir for lock' wrap", err)
	}
}

// TestAcquireSettingsLockOpenFileErrorWrapsWithOpen pins
// acquireSettingsLock's `OpenFile err → "open settings lock %s" wrap`
// arm (flock_unix.go:22-24). Reached when a directory sits at the lock
// path so OpenFile rejects with EISDIR. The wrap names "open settings
// lock" so operators see exactly which step failed.
func TestAcquireSettingsLockOpenFileErrorWrapsWithOpen(t *testing.T) {
	base := t.TempDir()
	lockPath := filepath.Join(base, "settings.lock")
	// Plant a directory at the lock path so OpenFile fails with EISDIR.
	if err := os.MkdirAll(lockPath, 0o755); err != nil {
		t.Fatalf("plant dir: %v", err)
	}

	_, err := acquireSettingsLock(lockPath, 100*time.Millisecond)
	if err == nil {
		t.Fatalf("acquireSettingsLock(dir-at-lockpath) err=nil; want OpenFile err")
	}
	if !strings.Contains(err.Error(), "open settings lock") {
		t.Errorf("err=%v; want 'open settings lock' wrap", err)
	}
}
