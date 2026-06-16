package embed_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embed"
)

func newSavableIndex(t *testing.T) *embed.Index {
	t.Helper()
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
		Ops: []catalog.Op{
			{OpID: "test.op", Title: "Test", Summary: "test summary"},
		},
	}
	idx, err := embed.Build(cat)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return idx
}

// TestIndexSaveMkdirAllErrorWraps pins Save's
// `MkdirAll err → return "embed: create index dir: %w"` arm
// (bm25.go:263-265). When the parent path is a regular file the
// MkdirAll call surfaces ENOTDIR, which Save MUST wrap so operators
// see the "create index dir:" prefix rather than a bare syscall error.
func TestIndexSaveMkdirAllErrorWraps(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	// blocker/sub cannot be a directory because blocker is a file → MkdirAll ENOTDIR.
	target := filepath.Join(blocker, "sub")
	idx := newSavableIndex(t)
	err := idx.Save(target)
	if err == nil {
		t.Fatal("Save(under-file) err=nil; want ENOTDIR wrap")
	}
	if !strings.Contains(err.Error(), "create index dir:") {
		t.Errorf("err=%q; want 'create index dir:' prefix", err.Error())
	}
}

// TestIndexSaveWriteFileErrorWraps pins Save's
// `os.WriteFile err → return "embed: write index.json: %w"` arm
// (bm25.go:303-305). Pre-create the target dir with mode 0o500
// (read+exec, no write) so MkdirAll on the existing dir is a no-op
// but WriteFile fails EACCES. Skipped when euid == 0 (root bypasses
// mode bits).
func TestIndexSaveWriteFileErrorWraps(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses unix mode bits")
	}
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "index")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatalf("mkdir 0o500: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	idx := newSavableIndex(t)
	err := idx.Save(dir)
	if err == nil {
		t.Fatal("Save(no-write dir) err=nil; want WriteFile EACCES wrap")
	}
	if !strings.Contains(err.Error(), "write index.json:") {
		t.Errorf("err=%q; want 'write index.json:' prefix", err.Error())
	}
}
