//go:build !windows

package auditlog

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// rotateLockedAtomic reads the clock at step 2, after the step-1 stat and
// before the rename. A clock hook that touches the filesystem is therefore
// a deterministic stand-in for a peer that rotated inside that window.
func racingClock(t *testing.T, armed *atomic.Bool, race func() error) func() time.Time {
	t.Helper()
	return func() time.Time {
		if armed.CompareAndSwap(true, false) {
			if err := race(); err != nil {
				t.Errorf("simulated peer rotation: %v", err)
			}
		}
		return time.Unix(1700000000, 0).UTC()
	}
}

func writerWithLiveFile(t *testing.T, dir string, now func() time.Time) *Writer {
	t.Helper()
	w, err := New(dir, WithClock(now))
	if err != nil {
		t.Fatalf("New(%s) = %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audit.jsonl"), []byte("{\"v\":1}\n"), 0o600); err != nil {
		t.Fatalf("seed audit.jsonl: %v", err)
	}
	return w
}

func TestRotateLockedAtomicPeerRotatedBeforeRename(t *testing.T) {
	dir := t.TempDir()
	var armed atomic.Bool
	w := writerWithLiveFile(t, dir, racingClock(t, &armed, func() error {
		return os.Remove(filepath.Join(dir, "audit.jsonl"))
	}))

	armed.Store(true)
	if err := w.rotateLockedAtomic(""); err != nil {
		t.Fatalf("rotateLockedAtomic = %v; want the lost rename treated as success", err)
	}

	info, err := os.Stat(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("stat audit.jsonl: %v; want it recreated", err)
	}
	if info.Size() != 0 {
		t.Fatalf("audit.jsonl size = %d; want a fresh empty file", info.Size())
	}
	entries, err := filepath.Glob(filepath.Join(dir, "audit.*.jsonl"))
	if err != nil {
		t.Fatalf("glob archives: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("archives = %v; want none, the rename never landed", entries)
	}
}

func TestRotateLockedAtomicRecreateAfterLostRenameFails(t *testing.T) {
	dir := t.TempDir()
	var armed atomic.Bool
	w := writerWithLiveFile(t, dir, racingClock(t, &armed, func() error {
		return os.RemoveAll(dir)
	}))

	armed.Store(true)
	err := w.rotateLockedAtomic("")
	if err == nil {
		t.Fatal("rotateLockedAtomic = nil; want the failed recreate to surface")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rotateLockedAtomic = %v; want a not-exist error from the recreate", err)
	}
}

func TestRotateUnderLockFallsBackWhenLockCannotOpen(t *testing.T) {
	dir := t.TempDir()
	w := writerWithLiveFile(t, dir, func() time.Time { return time.Unix(1700000000, 0).UTC() })
	// A lock path under a directory that does not exist fails to open with a
	// hard error, not errLockTimeout, so rotateUnderLock rotates unlocked.
	w.lockPath = filepath.Join(dir, "absent", "audit.jsonl.lock")

	if err := w.rotateUnderLock(50 * time.Millisecond); err != nil {
		t.Fatalf("rotateUnderLock = %v; want the unlocked fallback to rotate", err)
	}

	archives, err := filepath.Glob(filepath.Join(dir, "audit.*.jsonl"))
	if err != nil {
		t.Fatalf("glob archives: %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("archives = %v; want exactly one, proving the fallback rotated", archives)
	}
}

func TestEnforceMaxFilesLockedReadDirErrorBubblesUp(t *testing.T) {
	w := &Writer{dir: filepath.Join(t.TempDir(), "absent"), maxFiles: 5}
	err := w.enforceMaxFilesLocked()
	if err == nil {
		t.Fatal("enforceMaxFilesLocked = nil; want the unreadable directory to surface")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("enforceMaxFilesLocked = %v; want a not-exist error", err)
	}
}
