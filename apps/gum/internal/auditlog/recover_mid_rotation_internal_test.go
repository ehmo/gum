//go:build !windows

package auditlog

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecoverMidRotationNoLockEarlyReturn pins the most common branch:
// when audit.jsonl.lock is absent, recoverMidRotation is a no-op so
// New() doesn't slow the steady-state startup path.
func TestRecoverMidRotationNoLockEarlyReturn(t *testing.T) {
	dir := t.TempDir()
	w := &Writer{
		dir:      dir,
		path:     filepath.Join(dir, "audit.jsonl"),
		lockPath: filepath.Join(dir, "audit.jsonl.lock"),
	}
	w.recoverMidRotation() // must not panic or create files
	if _, err := os.Stat(w.path); !os.IsNotExist(err) {
		t.Errorf("audit.jsonl should still be absent; stat=%v", err)
	}
}

// TestRecoverMidRotationFileAlreadyExistsSkips pins the "another process
// already recovered" branch: lock present AND audit.jsonl present means
// no work to do, so the helper must not overwrite the existing file.
func TestRecoverMidRotationFileAlreadyExistsSkips(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "audit.jsonl.lock")
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(lockPath, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"seed":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := &Writer{
		dir:      dir,
		path:     path,
		lockPath: lockPath,
	}
	w.recoverMidRotation()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"seed":1}`+"\n" {
		t.Errorf("audit.jsonl mutated by recoverMidRotation: %q", got)
	}
}
