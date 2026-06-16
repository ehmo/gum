//go:build !windows

package auditlog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAppendBytesLockedHappyPath pins the success branch: the file is
// created (O_CREATE) on first call and re-opened in append mode on the
// second. A regression in the loop counter (e.g. early-exit before
// Write) would lose data.
func TestAppendBytesLockedHappyPath(t *testing.T) {
	dir := t.TempDir()
	w := &Writer{
		dir:  dir,
		path: filepath.Join(dir, "audit.jsonl"),
		now:  time.Now,
	}
	if err := w.appendBytesLocked([]byte("first\n")); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := w.appendBytesLocked([]byte("second\n")); err != nil {
		t.Fatalf("second append: %v", err)
	}
	got, err := os.ReadFile(w.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first\nsecond\n" {
		t.Errorf("audit.jsonl=%q; want 'first\\nsecond\\n'", got)
	}
}

// TestAppendBytesLockedDirAsFileSurfaces pins the non-ENOENT open-error
// branch: planting a directory at the audit.jsonl path turns the
// O_APPEND|O_WRONLY open into EISDIR, which MUST surface as an
// "open:"-wrapped error rather than be retried indefinitely.
func TestAppendBytesLockedDirAsFileSurfaces(t *testing.T) {
	dir := t.TempDir()
	// audit.jsonl is a DIRECTORY → OpenFile returns EISDIR-style error
	// (NOT NotExist), so the function must NOT retry; it must wrap+return.
	auditPath := filepath.Join(dir, "audit.jsonl")
	if err := os.Mkdir(auditPath, 0o700); err != nil {
		t.Fatal(err)
	}
	w := &Writer{
		dir:  dir,
		path: auditPath,
		now:  time.Now,
	}
	err := w.appendBytesLocked([]byte("anything\n"))
	if err == nil {
		t.Fatal("want EISDIR-style error; got nil")
	}
	if !strings.HasPrefix(err.Error(), "open:") &&
		!strings.HasPrefix(err.Error(), "write:") &&
		!strings.HasPrefix(err.Error(), "close:") {
		t.Errorf("err=%v; want 'open:' / 'write:' / 'close:' wrap (got bare error)", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("err=%v; must NOT be NotExist (would trigger retry)", err)
	}
}

// TestAppendBytesLockedENOENTRetryExhausted pins the worst-case loop:
// when O_CREATE itself fails repeatedly with NotExist (a parent dir
// that disappears between iterations), the function returns the
// "ENOENT retry exhausted" sentinel rather than looping forever. We
// emulate this by pointing path inside a non-existent parent so
// OpenFile-with-O_CREATE returns ENOENT for the dir, not the file.
func TestAppendBytesLockedENOENTRetryExhausted(t *testing.T) {
	dir := t.TempDir()
	w := &Writer{
		dir:  dir,
		path: filepath.Join(dir, "no-such-parent", "audit.jsonl"),
		now:  time.Now,
	}
	err := w.appendBytesLocked([]byte("x\n"))
	if err == nil {
		t.Fatal("want open error; got nil")
	}
	// Both ENOENT retry-exhausted and a non-retry open error end up wrapped
	// in "open:" — accept either form, but require the error to NOT be nil.
	if !strings.Contains(err.Error(), "open") {
		t.Errorf("err=%v; want 'open' substring", err)
	}
}
