package mcp

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestLoadFirstPartySchemaReadFileErrorReturnsFalse pins
// loadFirstPartySchema's `SchemaFS.ReadFile err → return nil, false`
// arm (schema_resource.go:107-109). Reached when the catalog
// references a ref via op.response_ref but the embedded
// gen/schemas/<ref>.json body doesn't exist (drift between catalog
// and embed at build time, or a hand-crafted test snapshot).
func TestLoadFirstPartySchemaReadFileErrorReturnsFalse(t *testing.T) {
	op := catalog.Op{
		OpID:         "synthetic.op",
		ResponseRef:  "nonexistent.schema.v1", // not in embedded SchemaFS
	}
	s := &Server{
		snapshot: &catalog.Catalog{
			CatalogSchemaVersion: 1,
			GeneratorVersion:     "test",
			Ops:                  []catalog.Op{op},
		},
	}
	body, ok := s.loadFirstPartySchema("nonexistent.schema.v1")
	if ok {
		t.Errorf("loadFirstPartySchema(missing-embedded) ok=true body=%d bytes; want (nil, false) on ReadFile err", len(body))
	}
	if body != nil {
		t.Errorf("body=%v; want nil on err arm", body)
	}
}

// TestLoadFirstPartySchemaUnreferencedReturnsFalse pins the
// `activeSnapshotReferencesRef → false → return nil, false` early-exit
// (schema_resource.go:103-105). A ref the catalog never mentions MUST
// NOT leak through — even if the file happens to exist in SchemaFS,
// the spec requires RESOURCE_NOT_FOUND for refs not surfaced by any
// active op/variant.
func TestLoadFirstPartySchemaUnreferencedReturnsFalse(t *testing.T) {
	// Empty catalog → no refs are ever active.
	s := &Server{snapshot: &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	}}
	body, ok := s.loadFirstPartySchema("any.ref")
	if ok {
		t.Errorf("loadFirstPartySchema(unreferenced)=true; want false (catalog-gated)")
	}
	if body != nil {
		t.Errorf("body=%v; want nil on unreferenced ref", body)
	}
}

// TestLoadPluginSchemaEmptyProfileDirReturnsFalse pins loadPluginSchema's
// `profileDir == "" → return nil, false` arm (schema_resource.go:128-130).
// Reached when neither XDG_DATA_HOME nor a usable HOME resolves —
// without the guard, the next filepath.Join would resolve to a
// CWD-relative path and silently read whatever file happens to exist.
func TestLoadPluginSchemaEmptyProfileDirReturnsFalse(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")

	s := &Server{profile: "default"}
	body, ok := s.loadPluginSchema("any.plugin.ref")
	if ok {
		t.Errorf("loadPluginSchema(no XDG/HOME)=true; want false (empty profileDir guard)")
	}
	if body != nil {
		t.Errorf("body=%v; want nil", body)
	}
}
