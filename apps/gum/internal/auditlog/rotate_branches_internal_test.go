//go:build !windows

package auditlog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRotateLockedAtomicRecreateOpenFileError pins the
// `cerr != nil && !errors.Is(cerr, os.ErrExist)` arm of the missing-file
// recreate branch: if the initial os.Stat fails with a not-exist-ish
// error (ENOTDIR, here) and the subsequent O_CREAT|O_EXCL also fails
// with the same ENOTDIR (which is NOT ErrExist), rotateLockedAtomic
// MUST surface the create error rather than silently returning nil.
func TestRotateLockedAtomicRecreateOpenFileError(t *testing.T) {
	w := newTestWriter(t, time.Now)
	// Plant a regular file inside the writer's dir, then reroute w.path
	// to a "subpath" of that regular file. os.Stat → ENOTDIR (satisfies
	// IsNotExist), then OpenFile → ENOTDIR (NOT ErrExist) → returns cerr.
	blocker := filepath.Join(w.dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.path = filepath.Join(blocker, "audit.jsonl")

	err := w.rotateLockedAtomic("")
	if err == nil {
		t.Fatal("want OpenFile recreate error; got nil")
	}
}

// TestRotateUnderLockTimeoutBubblesUp pins the `errors.Is(err,
// errLockTimeout)` arm of rotateUnderLock: when a peer (here: this
// goroutine holding a separate fd) already owns the flock, the
// timeout-bounded acquire fails with errLockTimeout, and
// rotateUnderLock MUST bubble that up unchanged so syncAppend can
// choose to skip rotation rather than fall back to lockless mode.
func TestRotateUnderLockTimeoutBubblesUp(t *testing.T) {
	w := newTestWriter(t, time.Now)

	// Acquire the rotation lock on a separate fd so a subsequent timed
	// acquire from rotateUnderLock blocks (and eventually times out).
	release, err := acquireRotationLock(w.lockPath, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer func() { _ = release() }()

	gotErr := w.rotateUnderLock(50 * time.Millisecond)
	if gotErr == nil {
		t.Fatal("want errLockTimeout; got nil")
	}
	if !errors.Is(gotErr, errLockTimeout) {
		t.Errorf("err=%v; want errLockTimeout bubbled up", gotErr)
	}
}
