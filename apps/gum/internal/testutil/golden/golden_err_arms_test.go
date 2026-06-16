package golden_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/testutil/golden"
)

// TestGoldenBytesReadErrorOtherThanNotExist pins Bytes's
// `!os.IsNotExist(err) → t.Fatalf` arm (golden.go:67-69). Reached when
// the path exists but reading fails for a non-missing reason — here
// the parent directory is chmod-stripped so opening the regular file
// inside fails with EACCES. Skipped when running as root because root
// bypasses mode-based permission checks.
func TestGoldenBytesReadErrorOtherThanNotExist(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("EACCES is not surfaced when running as root")
	}
	dir := t.TempDir()
	inner := filepath.Join(dir, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("plant inner: %v", err)
	}
	path := filepath.Join(inner, "fixture.txt")
	if err := os.WriteFile(path, []byte("real-bytes"), 0o644); err != nil {
		t.Fatalf("plant fixture: %v", err)
	}
	// Strip read+exec from the parent dir so os.Open(path) returns
	// EACCES — that's neither IsNotExist nor IsDir.
	if err := os.Chmod(inner, 0o000); err != nil {
		t.Fatalf("chmod inner: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(inner, 0o755) })

	var msgs []string
	captured := &capturingTB{TB: t, onFatal: func(m string) {
		msgs = append(msgs, m)
	}}
	golden.Bytes(captured, path, []byte("ignored"))
	if len(msgs) == 0 {
		t.Fatalf("expected Bytes(EACCES) to call Fatalf at least once")
	}
	joined := strings.Join(msgs, "|")
	if !strings.Contains(joined, "golden: read") {
		t.Errorf("expected 'golden: read' wrap in at least one Fatalf; got %q", joined)
	}
}

// TestGoldenWriteGoldenMkdirAllErrorPropagates pins writeGolden's
// `MkdirAll err → t.Fatalf("golden: mkdir ...")` arm (golden.go:90-92).
// Reached when a regular file sits at the parent-dir path so MkdirAll
// fails with ENOTDIR. The Bytes seed path is triggered because the
// path itself doesn't exist yet (ReadFile returns ErrNotExist), so
// writeGolden runs and MkdirAll trips.
func TestGoldenWriteGoldenMkdirAllErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	// Parent directory chain traverses through `blocker` which is a file.
	path := filepath.Join(blocker, "nested", "golden.txt")

	// capturingTB doesn't FailNow, so when MkdirAll fails Fatalf is
	// intercepted and code continues to WriteFile (which then ALSO fails
	// because the parent dir wasn't created). Collect every Fatalf
	// message and check that the mkdir-err arm fired at least once.
	var msgs []string
	captured := &capturingTB{TB: t, onFatal: func(m string) {
		msgs = append(msgs, m)
	}}
	golden.Bytes(captured, path, []byte("data"))
	if len(msgs) == 0 {
		t.Fatalf("expected Bytes(blocked-parent) to call Fatalf at least once")
	}
	joined := strings.Join(msgs, "|")
	if !strings.Contains(joined, "golden: mkdir") {
		t.Errorf("expected 'golden: mkdir' wrap in at least one Fatalf; got %q", joined)
	}
}
