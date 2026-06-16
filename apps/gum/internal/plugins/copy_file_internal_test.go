package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopyFileSourceMissingFails pins the os.Open error branch: a
// missing source MUST surface immediately so callers don't write an
// empty destination file.
func TestCopyFileSourceMissingFails(t *testing.T) {
	dir := t.TempDir()
	err := copyFile(filepath.Join(dir, "missing.bin"), filepath.Join(dir, "dst"), 0o600)
	if err == nil {
		t.Fatal("want open error; got nil")
	}
}

// TestCopyFileMkdirAllFailsWhenParentIsFile pins the os.MkdirAll error
// branch: when the destination's parent path is a regular file, MkdirAll
// can't create it as a directory and the error must propagate.
func TestCopyFileMkdirAllFailsWhenParentIsFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Plant a regular file where the destination wants a directory.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	// dst path traverses through the regular file → MkdirAll fails.
	dst := filepath.Join(blocker, "sub", "dst")
	err := copyFile(src, dst, 0o600)
	if err == nil {
		t.Fatal("want MkdirAll error; got nil")
	}
}

// TestCopyFileOpenDstFailsWhenDstIsDir pins the os.OpenFile error
// branch: a directory at the destination path makes O_WRONLY|O_CREATE
// return EISDIR-style error, which must propagate.
func TestCopyFileOpenDstFailsWhenDstIsDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Plant a directory where the destination wants a file.
	dstAsDir := filepath.Join(dir, "dstdir")
	if err := os.Mkdir(dstAsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	err := copyFile(src, dstAsDir, 0o600)
	if err == nil {
		t.Fatal("want EISDIR-style error; got nil")
	}
}

// TestCopyFileHappyPath pins the success branch end-to-end: contents
// arrive byte-identical and the mode matches.
func TestCopyFileHappyPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	want := []byte("hello world\n")
	if err := os.WriteFile(src, want, 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "dst.bin")
	if err := copyFile(src, dst, 0o640); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("contents=%q; want %q", got, want)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("mode=%o; want 0640", info.Mode().Perm())
	}
}
