package embed_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/embed"
)

// TestLoadReadErrorWraps pins the os.ReadFile error arm: a missing
// index.json must surface the wrapped "embed: read index.json" prefix
// so callers can distinguish I/O failure from a corrupt file.
func TestLoadReadErrorWraps(t *testing.T) {
	_, err := embed.Load(t.TempDir()) // no index.json planted
	if err == nil {
		t.Fatal("want read error; got nil")
	}
	if !strings.Contains(err.Error(), "read index.json") {
		t.Errorf("err=%v; want 'read index.json' wrap", err)
	}
}

// TestLoadUnparseableJSONWrapsCorrupt pins the json.Unmarshal arm:
// garbage bytes must wrap ErrIndexCorrupt so the BM25 layer can react
// to a "rebuild the index" recovery path.
func TestLoadUnparseableJSONWrapsCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := embed.Load(dir)
	if err == nil {
		t.Fatal("want decode error; got nil")
	}
	if !errors.Is(err, embed.ErrIndexCorrupt) {
		t.Errorf("err=%v; want ErrIndexCorrupt wrap", err)
	}
}

// TestLoadWrongVersionWrapsCorrupt pins the version-mismatch arm:
// indexes from a future schema must be rejected as corrupt rather than
// silently used (which would skew BM25 scoring against unknown fields).
func TestLoadWrongVersionWrapsCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.json"),
		[]byte(`{"version":"bm25-only-v999","docs":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := embed.Load(dir)
	if err == nil {
		t.Fatal("want version error; got nil")
	}
	if !errors.Is(err, embed.ErrIndexCorrupt) {
		t.Errorf("err=%v; want ErrIndexCorrupt wrap", err)
	}
	if !strings.Contains(err.Error(), `"bm25-only-v999"`) {
		t.Errorf("err=%v; want offending version echoed", err)
	}
}

// TestLoadMissingDocsWrapsCorrupt pins the "docs == nil" guard: a
// well-versioned file that omits the docs array must be rejected so
// downstream search can't operate on a nil slice.
func TestLoadMissingDocsWrapsCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.json"),
		[]byte(`{"version":"bm25-only-v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := embed.Load(dir)
	if err == nil {
		t.Fatal("want missing-docs error; got nil")
	}
	if !errors.Is(err, embed.ErrIndexCorrupt) {
		t.Errorf("err=%v; want ErrIndexCorrupt wrap", err)
	}
	if !strings.Contains(err.Error(), "missing docs") {
		t.Errorf("err=%v; want 'missing docs' diag", err)
	}
}
