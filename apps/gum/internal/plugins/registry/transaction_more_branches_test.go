package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteTransactionMkdirAllErrorPropagates pins the MkdirAll arm: if
// the profileDir cannot be created (e.g. its would-be parent is a
// regular file), WriteTransaction MUST surface a "registry: mkdir
// profile dir" wrap rather than panicking deeper in the lock-acquire
// path.
func TestWriteTransactionMkdirAllErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	// Plant a regular file where the would-be profile dir's parent must
	// be a directory — MkdirAll then fails with ENOTDIR on the leaf.
	parent := filepath.Join(dir, "notadir")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(filepath.Join(parent, "profile")) // <regularfile>/profile/

	err := r.WriteTransaction(context.Background(), func(*Files) error { return nil })
	if err == nil {
		t.Fatal("want mkdir error; got nil")
	}
	if !strings.Contains(err.Error(), "registry: mkdir profile dir") {
		t.Errorf("err=%v; want 'registry: mkdir profile dir' wrap", err)
	}
}

// TestWriteTransactionLoadErrorPropagates pins the r.Load() arm: a
// corrupt catalog file in the profile dir surfaces an unsupported-
// schema-version error from Load that WriteTransaction MUST forward
// verbatim (not swallow as "empty registry" and proceed to overwrite).
func TestWriteTransactionLoadErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	// Plant an unsupported-schema catalog so Load returns the
	// ErrUnsupportedPluginCatalogSchemaVersion wrap exercised in
	// registry/load_branches_internal_test.go.
	if err := os.WriteFile(filepath.Join(dir, CatalogFilename),
		[]byte(`{"plugin_catalog_schema_version":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(dir)
	err := r.WriteTransaction(context.Background(), func(*Files) error { return nil })
	if err == nil {
		t.Fatal("want load error; got nil")
	}
}

// TestWriteTransactionStateGenerationHigherWins pins the
// `files.State.InstallGeneration > prev` arm: when the state file's
// generation is ahead of the lock's (an inconsistent on-disk pair),
// the next generation MUST be derived from state — otherwise we'd
// stomp a newer-generation row with an older one and silently rewind.
func TestWriteTransactionStateGenerationHigherWins(t *testing.T) {
	dir := t.TempDir()
	// Plant a state file at generation 42; lock stays at default 0.
	if err := os.WriteFile(filepath.Join(dir, StateFilename),
		[]byte(`{"plugin_state_schema_version":1,"install_generation":42,"plugins":[]}`),
		0o600); err != nil {
		t.Fatal(err)
	}
	r := New(dir)
	if err := r.WriteTransaction(context.Background(), func(*Files) error { return nil }); err != nil {
		t.Fatalf("WriteTransaction: %v", err)
	}
	// New state file MUST be at generation 43 (max(0, 42) + 1), not 1.
	data, err := os.ReadFile(filepath.Join(dir, StateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"install_generation": 43`) {
		t.Errorf("state file does not have generation 43:\n%s", string(data))
	}
}
