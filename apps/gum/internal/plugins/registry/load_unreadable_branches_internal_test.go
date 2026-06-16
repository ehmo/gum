package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadLockReadErrorPropagates pins Load's
// `readIfExists(LockPath) err → return err` arm
// (transaction.go:93-95). A chmod 0o000 lock file makes ReadFile
// surface EACCES (not ENOENT), so readIfExists wraps it as
// "registry: read ..." and Load propagates verbatim.
func TestLoadLockReadErrorPropagates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix mode bits")
	}
	dir := t.TempDir()
	lockPath := filepath.Join(dir, LockFilename)
	if err := os.WriteFile(lockPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("plant lock: %v", err)
	}
	if err := os.Chmod(lockPath, 0o000); err != nil {
		t.Fatalf("chmod lock: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockPath, 0o600) })

	r := New(dir)
	_, err := r.Load()
	if err == nil {
		t.Fatal("Load(unreadable lock) err=nil; want EACCES wrap")
	}
	if !strings.Contains(err.Error(), "registry: read") {
		t.Errorf("err=%q; want 'registry: read' prefix", err.Error())
	}
}

// TestLoadStateReadErrorPropagates pins the analogous arm for
// plugin-state.json (transaction.go:102-104).
func TestLoadStateReadErrorPropagates(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix mode bits")
	}
	dir := t.TempDir()
	statePath := filepath.Join(dir, StateFilename)
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("plant state: %v", err)
	}
	if err := os.Chmod(statePath, 0o000); err != nil {
		t.Fatalf("chmod state: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(statePath, 0o600) })

	r := New(dir)
	_, err := r.Load()
	if err == nil {
		t.Fatal("Load(unreadable state) err=nil; want EACCES wrap")
	}
	if !strings.Contains(err.Error(), "registry: read") {
		t.Errorf("err=%q; want 'registry: read' prefix", err.Error())
	}
}
