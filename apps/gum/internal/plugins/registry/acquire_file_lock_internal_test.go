//go:build !windows

package registry

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestAcquireFileLockHappyPath pins the success branch: a single
// caller acquires and releases the lock without contention.
func TestAcquireFileLockHappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.lock")
	release, err := acquireFileLock(path, 250*time.Millisecond)
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

// TestAcquireFileLockOpenError pins the OpenFile error branch: pointing
// the lock at a non-existent directory surfaces a wrapped
// "open install lock" error rather than panicking on the nil fd.
func TestAcquireFileLockOpenError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "install.lock")
	_, err := acquireFileLock(path, 50*time.Millisecond)
	if err == nil {
		t.Fatal("want open error; got nil")
	}
}

// TestAcquireFileLockTimeoutFromContention pins the EWOULDBLOCK+deadline
// arm: when one fd holds the lock and a second tries with a tiny
// timeout, the second call must wrap ErrLockTimeout so the registry
// caller can recover via the spec §8.7 "lock-contention" path.
func TestAcquireFileLockTimeoutFromContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.lock")
	release, err := acquireFileLock(path, 250*time.Millisecond)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer func() { _ = release() }()

	var wg sync.WaitGroup
	wg.Add(1)
	var secondErr error
	go func() {
		defer wg.Done()
		_, secondErr = acquireFileLock(path, 60*time.Millisecond)
	}()
	wg.Wait()

	if secondErr == nil {
		t.Fatal("want lock-timeout; got nil")
	}
	if !errors.Is(secondErr, ErrLockTimeout) {
		t.Errorf("err=%v; want ErrLockTimeout wrap", secondErr)
	}
}
