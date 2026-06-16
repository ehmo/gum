package plugins_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestSchemaRefsFromCatalogSortByRefThenOwner pins both sort-less
// arms (schema_ref.go:57-59 ref-differ, 60 ref-equal owner tiebreak).
// Reached by seeding two rows with the SAME ref but different
// owners (forces the owner-tiebreak path) plus a third row with a
// different ref (forces the primary ref comparison). The expected
// output ordering is deterministic: refs ascending, then owners
// ascending.
func TestSchemaRefsFromCatalogSortByRefThenOwner(t *testing.T) {
	t.Parallel()
	variants := []any{
		map[string]any{
			"owner_plugin":  "zeta-plugin",
			"schema_hashes": map[string]any{"#/types/X": "sha256:aaaa"},
		},
		map[string]any{
			"owner_plugin":  "alpha-plugin",
			"schema_hashes": map[string]any{"#/types/X": "sha256:aaaa"},
		},
		map[string]any{
			"owner_plugin":  "beta-plugin",
			"schema_hashes": map[string]any{"#/types/Z": "sha256:bbbb"},
		},
	}
	out := plugins.SchemaRefsFromCatalog(variants)
	if len(out) != 3 {
		t.Fatalf("len=%d; want 3", len(out))
	}
	// Expected: (#/types/X, alpha-plugin), (#/types/X, zeta-plugin), (#/types/Z, beta-plugin)
	want := []struct{ ref, owner string }{
		{"#/types/X", "alpha-plugin"},
		{"#/types/X", "zeta-plugin"},
		{"#/types/Z", "beta-plugin"},
	}
	for i, w := range want {
		if out[i].Ref != w.ref || out[i].OwnerPlugin != w.owner {
			t.Errorf("out[%d]={%s,%s}; want {%s,%s}", i, out[i].Ref, out[i].OwnerPlugin, w.ref, w.owner)
		}
	}
}

// TestDetectSchemaRefCollisionExistingInventorySelfCollision pins
// the `existing self-collision → error` arm (schema_ref.go:80-85).
// Reached when the `existing` slice itself contains the same ref
// with divergent hashes — the function MUST surface this as a
// collision so the operator knows the registry needs repair, even
// before considering the candidate.
func TestDetectSchemaRefCollisionExistingInventorySelfCollision(t *testing.T) {
	t.Parallel()
	existing := []plugins.SchemaRef{
		{Ref: "#/types/X", Hash: "sha256:aaa", OwnerPlugin: "plugin-a"},
		{Ref: "#/types/X", Hash: "sha256:bbb", OwnerPlugin: "plugin-b"}, // divergent
	}
	candidate := []plugins.SchemaRef{
		{Ref: "#/types/Y", Hash: "sha256:ccc", OwnerPlugin: "plugin-c"},
	}
	err := plugins.DetectSchemaRefCollision(existing, candidate)
	if err == nil {
		t.Fatal("DetectSchemaRefCollision(self-collision) err=nil; want ErrSchemaRefCollision")
	}
	if !errors.Is(err, plugins.ErrSchemaRefCollision) {
		t.Errorf("err=%v; want ErrSchemaRefCollision wrap", err)
	}
}
