package initpkg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAtomicWriteHappyPath covers the success branch: data lands at the
// destination with the requested mode, and no temp files leak behind.
func TestAtomicWriteHappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	want := []byte(`{"k":"v"}`)

	if err := atomicWrite(path, want, 0o600); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got=%q; want=%q", got, want)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode=%o; want 0600", fi.Mode().Perm())
	}

	// Confirm no .gum-init-*.tmp leftovers in the directory.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if name := e.Name(); name != "out.json" {
			t.Errorf("unexpected leftover: %s", name)
		}
	}
}

// TestAtomicWriteTempfileFailure covers the os.CreateTemp branch: when
// the destination directory does not exist, the wrapper must surface
// the wrapped "initpkg: tempfile" error without panicking.
func TestAtomicWriteTempfileFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "out.json")
	err := atomicWrite(path, []byte("x"), 0o644)
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

// TestAtomicWriteOverwriteExisting covers the os.Rename branch when a
// file already exists at the destination — rename must replace it
// atomically without erroring on Unix-likes.
func TestAtomicWriteOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("got=%q; want=new", got)
	}
}

// TestAtomicWriteRenameFailureCleansTemp covers the os.Rename error
// branch: when the destination is a non-empty directory, rename fails
// with EISDIR/ENOTEMPTY. The wrapper must surface the wrapped error
// and not leave a .gum-init-*.tmp file behind.
func TestAtomicWriteRenameFailureCleansTemp(t *testing.T) {
	dir := t.TempDir()
	destDir := filepath.Join(dir, "iam-a-dir")
	if err := os.Mkdir(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a file inside the dir so it's non-empty (rename-onto-dir is
	// rejected by the kernel even more aggressively when destination is
	// occupied).
	if err := os.WriteFile(filepath.Join(destDir, "occupant"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := atomicWrite(destDir, []byte("payload"), 0o600)
	if err == nil {
		t.Fatal("expected rename error onto non-empty dir; got nil")
	}
	// No .gum-init-*.tmp leftovers in the parent dir.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if name := e.Name(); name != "iam-a-dir" {
			t.Errorf("temp file leaked: %s", name)
		}
	}
}
