//go:build !windows

package registry

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFileExistsReadErrorReturnsFalse pins the `err != nil → false`
// arm: when readIfExists fails with a non-ErrNotExist error (here,
// EISDIR from passing a directory path), fileExists MUST report
// false rather than panic or return true on the assumption that any
// non-ENOENT outcome means "present."
func TestFileExistsReadErrorReturnsFalse(t *testing.T) {
	dir := t.TempDir() // dir exists but ReadFile(dir) fails with EISDIR
	if got := fileExists(dir); got {
		t.Errorf("fileExists(<dir>)=true; want false (EISDIR → read error → false)")
	}
}

// TestWriteTransactionFileLockTimeoutPropagates pins the
// acquireFileLock err arm: when a peer (a separate fd held by this
// test) already owns plugins.install.lock, WriteTransaction MUST
// surface the lock error (here: ErrLockTimeout) rather than block
// indefinitely or stomp the registry without the lock.
func TestWriteTransactionFileLockTimeoutPropagates(t *testing.T) {
	dir := t.TempDir()

	// Hold the install lock on a separate fd via acquireFileLock itself —
	// this mirrors the cross-process scenario in spec §8.7 (same kernel
	// flock semantics apply across distinct fds even in one process).
	release, err := acquireFileLock(filepath.Join(dir, "plugins.install.lock"), 100*time.Millisecond)
	if err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	defer func() { _ = release() }()

	r := New(dir)
	r.lockTimeout = 50 * time.Millisecond

	err = r.WriteTransaction(context.Background(), func(*Files) error { return nil })
	if err == nil {
		t.Fatal("want lock error; got nil")
	}
	if !strings.Contains(err.Error(), "flock") {
		t.Errorf("err=%v; want 'flock' wrap", err)
	}
}
