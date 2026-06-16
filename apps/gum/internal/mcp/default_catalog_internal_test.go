package mcp

import (
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestDefaultCatalogEmptyReturnsNil pins the "no embedded snapshot" branch
// of defaultCatalog so unofficial builds (built without the catalog blob)
// see a nil catalog rather than a panic on json.Unmarshal of empty input.
func TestDefaultCatalogEmptyReturnsNil(t *testing.T) {
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = nil

	if c := defaultCatalog(); c != nil {
		t.Errorf("defaultCatalog()=%+v; want nil for empty snapshot", c)
	}
}

// TestDefaultCatalogUnparseableReturnsNil pins the unmarshal-error branch:
// a corrupt embedded catalog (e.g. truncated build) MUST NOT crash the
// MCP server; defaultCatalog returns nil and callers fall back to the
// empty-catalog path.
func TestDefaultCatalogUnparseableReturnsNil(t *testing.T) {
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = []byte("{not valid json")

	if c := defaultCatalog(); c != nil {
		t.Errorf("defaultCatalog()=%+v; want nil for corrupt snapshot", c)
	}
}
