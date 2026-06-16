//go:build !windows

package auditlog

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestAcquireRotationLockHappyPath pins the success branch: a single
// caller acquires and releases the lock without contention.
func TestAcquireRotationLockHappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rotation.lock")
	release, err := acquireRotationLock(path, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if release == nil {
		t.Fatal("release is nil; want callable")
	}
	if err := release(); err != nil {
		t.Errorf("release: %v", err)
	}
}

// TestAcquireRotationLockOpenError pins the OpenFile-failure branch:
// pointing the lock at a path inside a non-existent dir surfaces a
// wrapped "open rotation lock" error.
func TestAcquireRotationLockOpenError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "rotation.lock")
	_, err := acquireRotationLock(path, 50*time.Millisecond)
	if err == nil {
		t.Fatal("want open error; got nil")
	}
}

// TestAcquireRotationLockTimeoutFromContention pins the deadline
// branch: when one goroutine holds the lock and a second tries to
// acquire it with a tiny timeout, the second call must return
// errLockTimeout (not a generic "flock" error) so the rotation
// driver can fall back to the spec §11 contention path.
func TestAcquireRotationLockTimeoutFromContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rotation.lock")
	release, err := acquireRotationLock(path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer func() { _ = release() }()

	// In a second goroutine: a process-distinct flock attempt would
	// normally be required, but flock is per-fd on the same process —
	// opening a *separate* fd via acquireRotationLock yields the same
	// blocking behaviour at the kernel level.
	var wg sync.WaitGroup
	wg.Add(1)
	var secondErr error
	go func() {
		defer wg.Done()
		_, secondErr = acquireRotationLock(path, 60*time.Millisecond)
	}()
	wg.Wait()
	if secondErr == nil {
		t.Fatal("second acquire returned nil; want errLockTimeout")
	}
	if !errors.Is(secondErr, errLockTimeout) {
		t.Errorf("err=%v; want errLockTimeout (so caller can recover)", secondErr)
	}
}
