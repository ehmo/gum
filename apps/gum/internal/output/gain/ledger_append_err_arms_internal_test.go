package gain

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAppendWriteErrorWrapsWriteEntry pins Append's
// `l.file.Write err → return 'write entry' wrap` arm (ledger.go:387-389).
// Reached when the underlying file handle is closed mid-life (e.g., a
// readonly-fs unmount or a parallel Close on the same handle). We close
// l.file directly (bypassing the Ledger.Close path that would also nil
// the field) so Append's Write call gets EBADF.
func TestAppendWriteErrorWrapsWriteEntry(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLedger(filepath.Join(dir, "ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	// Close the underlying file without nil-ing the field; Append must
	// surface the write err with the canonical "write entry" wrap.
	if err := l.file.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}

	err = l.Append(Entry{OpID: "test.op", RawTokens: 10, ShapedTokens: 5})
	if err == nil {
		t.Fatalf("Append on closed file err=nil; want write err")
	}
	if !strings.Contains(err.Error(), "write entry") {
		t.Errorf("err=%v; want 'write entry' wrap", err)
	}
}
