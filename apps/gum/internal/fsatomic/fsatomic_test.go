package fsatomic

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileCreatesWithModeAndContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k")
	if err := WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q; want hello", got)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v; want 0600", fi.Mode().Perm())
		}
	}
}

func TestWriteFileReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "k")
	if err := os.WriteFile(path, []byte("old-and-longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q; want new", got)
	}
	// No stray temp files left behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir has %d entries; want 1 (temp file leaked?)", len(entries))
	}
}

func TestWriteFileErrorsWhenDirMissing(t *testing.T) {
	if err := WriteFile(filepath.Join(t.TempDir(), "nope", "k"), []byte("x"), 0o600); err == nil {
		t.Error("expected error writing into a nonexistent directory")
	}
}

func TestWriteFileErrorsWhenTargetIsDir(t *testing.T) {
	// path is an existing directory → the final rename(tmp, path) fails, which
	// exercises the rename-error cleanup branch.
	dir := t.TempDir()
	if err := WriteFile(dir, []byte("x"), 0o600); err == nil {
		t.Error("expected error renaming over an existing directory")
	}
	// The temp file must have been cleaned up (only the dir itself remains).
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("temp file leaked into target dir: %v", entries)
	}
}

func TestOpenNoFollowReadsRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenNoFollow(path)
	if err != nil {
		t.Fatalf("OpenNoFollow: %v", err)
	}
	defer func() { _ = f.Close() }()
	b, _ := io.ReadAll(f)
	if string(b) != "data" {
		t.Errorf("content = %q; want data", b)
	}
}

func TestOpenNoFollowRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("O_NOFOLLOW is a no-op on this platform")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	f, err := OpenNoFollow(link)
	if err == nil {
		_ = f.Close()
		t.Error("OpenNoFollow followed a symlink; want refusal")
	}
}
