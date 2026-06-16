package main

import (
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestLoadCatalogEmptyEmbedReturnsNil pins the early-return branch: if the
// embedded catalog blob is empty (a missing or stripped binary asset), the
// helper must surface nil rather than calling json.Unmarshal on []byte{}.
// Restoring the previous bytes via t.Cleanup keeps later tests untouched.
func TestLoadCatalogEmptyEmbedReturnsNil(t *testing.T) {
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = nil

	if got := loadCatalog(); got != nil {
		t.Errorf("loadCatalog()=%+v; want nil on empty embed", got)
	}
}

// TestLoadCatalogUnparseableEmbedReturnsNil pins the unmarshal-error
// branch: a corrupt or non-JSON blob must collapse to nil so the CLI
// degrades to "no catalog" rather than panicking on the parse.
func TestLoadCatalogUnparseableEmbedReturnsNil(t *testing.T) {
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = []byte("{not valid json")

	if got := loadCatalog(); got != nil {
		t.Errorf("loadCatalog()=%+v; want nil on parse error", got)
	}
}
