package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestDescribeCmdCatalogNotLoadedSurfacesError pins the
// `loadCatalog() == nil → return "OP_NOT_FOUND: <id> (catalog not
// loaded)"` arm. In production the embedded catalog is always
// populated, but a deployment that strips the asset (or a future
// hermetic-build mode that lazily provisions the catalog) MUST
// surface a clean error rather than a nil-deref panic. The "(catalog
// not loaded)" suffix distinguishes this from the regular
// "OP_NOT_FOUND: <id>" (no suffix) emitted when the op truly isn't
// in a populated catalog — operators can grep on the suffix to
// diagnose deployment misconfig vs. typo.
func TestDescribeCmdCatalogNotLoadedSurfacesError(t *testing.T) {
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = nil

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"describe", "gum.read"})

	err := root.Execute()
	if err == nil {
		t.Fatalf("describe (catalog stripped)=nil err; want OP_NOT_FOUND surface\nstdout:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "OP_NOT_FOUND: gum.read") {
		t.Errorf("err=%q; want 'OP_NOT_FOUND: gum.read' prefix", err)
	}
	if !strings.Contains(err.Error(), "catalog not loaded") {
		t.Errorf("err=%q; want '(catalog not loaded)' suffix (distinguishes from typo path)", err)
	}
}
