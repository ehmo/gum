package gain

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteHeaderLockedWriteErrorWraps pins the file.Write error branch of
// writeHeaderLocked: closing the underlying file before invocation makes
// Write return EBADF; the helper must wrap with the "gain: write header"
// prefix instead of returning the bare syscall error.
func TestWriteHeaderLockedWriteErrorWraps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")
	l, err := NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	// Force the underlying *os.File closed so the next Write returns EBADF.
	if cerr := l.file.Close(); cerr != nil {
		t.Fatalf("pre-close file: %v", cerr)
	}

	err = l.writeHeaderLocked()
	if err == nil {
		t.Fatal("want write error on closed fd; got nil")
	}
	if !strings.Contains(err.Error(), "gain: write header") {
		t.Errorf("err=%v; want 'gain: write header' wrap", err)
	}
}
