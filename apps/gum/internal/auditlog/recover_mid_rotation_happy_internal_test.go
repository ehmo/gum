//go:build !windows

package auditlog

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecoverMidRotationLockUnavailableLogs pins the acquireRotationLock
// failure arm: when lockPath is a directory (not a regular file),
// O_CREATE|O_RDWR fails with EISDIR. The helper must swallow the error
// (slog.Warn + return) so callers see a no-op, not a panic.
func TestRecoverMidRotationLockUnavailableLogs(t *testing.T) {
	dir := t.TempDir()
	// Plant a DIRECTORY at the lock path so OpenFile fails with EISDIR.
	lockPath := filepath.Join(dir, "audit.jsonl.lock")
	if err := os.Mkdir(lockPath, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "audit.jsonl") // absent
	w := &Writer{dir: dir, path: path, lockPath: lockPath}

	w.recoverMidRotation() // must not panic, must not create audit.jsonl

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("audit.jsonl should still be absent (lock acquire failed); stat=%v", err)
	}
}

// TestRecoverMidRotationOCreateErrorLogs pins the O_CREATE failure arm:
// when the parent directory for w.path doesn't exist, OpenFile fails
// with ENOENT after the lock is held. The helper must log and return
// rather than crash.
func TestRecoverMidRotationOCreateErrorLogs(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "audit.jsonl.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	// Path points through a missing parent directory: stat returns
	// ENOENT (passes the absent-file guard), OpenFile then fails ENOENT.
	path := filepath.Join(dir, "missing-parent", "audit.jsonl")
	w := &Writer{dir: dir, path: path, lockPath: lockPath}

	w.recoverMidRotation()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("audit.jsonl unexpectedly created at %s", path)
	}
}

// TestRecoverMidRotationCreatesMissingFile pins the full recovery branch:
// when the .lock sentinel is present but audit.jsonl is absent (the
// spec §11 mid-rotation crash shape), the helper acquires the lock and
// O_CREATEs an empty audit.jsonl so subsequent appends don't have to
// re-roll through the recovery path.
func TestRecoverMidRotationCreatesMissingFile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "audit.jsonl.lock")
	path := filepath.Join(dir, "audit.jsonl")

	// Plant the lock sentinel but no audit.jsonl: the precise crash shape
	// recoverMidRotation exists to repair.
	if err := os.WriteFile(lockPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	w := &Writer{
		dir:      dir,
		path:     path,
		lockPath: lockPath,
	}
	w.recoverMidRotation()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("audit.jsonl missing after recovery: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("audit.jsonl size=%d; want 0 (recovery is empty O_CREAT)", info.Size())
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("audit.jsonl mode=%o; want 0600", info.Mode().Perm())
	}
}
