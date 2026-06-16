package auditlog

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// makeRotationWriter returns a minimally-populated *Writer suitable for
// driving rotateLockedAtomic directly. It bypasses New() so the test can
// plant pathological filesystem states that New's MkdirAll would refuse.
func makeRotationWriter(dir, path string) *Writer {
	return &Writer{
		dir:      dir,
		path:     path,
		lockPath: filepath.Join(dir, "audit.jsonl.lock"),
		maxFiles: 5,
		mu:       sync.Mutex{},
		now:      func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
}

// TestRotateLockedAtomicReturnsOpenFileErrOnMissingPathWithBadParent pins
// rotateLockedAtomic's `OpenFile err && !ErrExist → return cerr` arm
// (writer.go:548-550). Reached when w.path doesn't exist AND the parent
// directory also doesn't exist, so the recreate-empty step's OpenFile
// trips with ENOENT (not ErrExist).
func TestRotateLockedAtomicReturnsOpenFileErrOnMissingPathWithBadParent(t *testing.T) {
	base := t.TempDir()
	// Point w.path into a non-existent subdir so both the Stat and the
	// recreate OpenFile fail.
	w := makeRotationWriter(filepath.Join(base, "ghost"), filepath.Join(base, "ghost", "audit.jsonl"))

	err := w.rotateLockedAtomic("")
	if err == nil {
		t.Fatalf("rotateLockedAtomic(missing path + missing parent) err=nil; want OpenFile err")
	}
	// Bubbles the raw OpenFile error (no extra wrap on this arm — caller
	// passes it through verbatim).
	if !strings.Contains(err.Error(), "no such file or directory") &&
		!strings.Contains(err.Error(), "not exist") {
		t.Errorf("err=%v; want ENOENT-class error from OpenFile on missing parent", err)
	}
}

// TestRotateLockedAtomicReadOnlyDirReturnsError documents that a
// read-only profile directory causes rotation to fail at the rename
// step. The exact wrap depends on which OS syscall trips first
// (rename, chmod, OpenFile), so this test asserts only that a non-nil
// error surfaces — distinguishes the broad failure-class from the
// happy path tested elsewhere.
func TestRotateLockedAtomicReadOnlyDirReturnsError(t *testing.T) {
	base := t.TempDir()
	livePath := filepath.Join(base, "audit.jsonl")
	if err := os.WriteFile(livePath, []byte("payload\n"), 0o600); err != nil {
		t.Fatalf("plant audit.jsonl: %v", err)
	}
	w := makeRotationWriter(base, livePath)

	if err := os.Chmod(base, 0o500); err != nil {
		t.Fatalf("chmod readonly: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(base, 0o700) })

	if err := w.rotateLockedAtomic(""); err == nil {
		t.Skip("read-only dir did not cause rotation to fail on this platform; skip")
	}
}
