package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelectGenerationPropagatesLoadError pins recover.go:25-27 — the
// `files, err := r.Load(); if err != nil { return Generation{}, err }` arm.
// A registry whose plugin-catalog.json exists but is malformed makes Load
// fail at catalog.LoadPluginCatalog; SelectGeneration must surface that
// error rather than proceed to the presence/quorum logic.
func TestSelectGenerationPropagatesLoadError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Write a syntactically broken catalog file so LoadPluginCatalog errors.
	if err := os.WriteFile(filepath.Join(dir, CatalogFilename), []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("seed malformed catalog: %v", err)
	}

	reg := New(dir)
	_, err := reg.SelectGeneration()
	if err == nil {
		t.Fatal("SelectGeneration err = nil; want Load error from malformed catalog")
	}
	// Sanity: the error should not be the empty zero-value masquerading; it
	// must carry a parse-failure signal.
	if strings.TrimSpace(err.Error()) == "" {
		t.Errorf("err message empty; want a Load/parse error")
	}
}
