package mcp

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestActiveSnapshotReferencesRefNilCatalog pins the `c == nil → false`
// arm (schema_resource.go:166-168). When the catalog isn't loaded yet
// the helper must answer false rather than panic on c.Ops.
func TestActiveSnapshotReferencesRefNilCatalog(t *testing.T) {
	t.Parallel()
	if activeSnapshotReferencesRef(nil, "#/types/X") {
		t.Error("got=true; want false for nil catalog")
	}
}

// TestActiveSnapshotReferencesRefEmptyRef pins the `ref == "" → false`
// arm (same guard at schema_resource.go:166-168). An empty ref query
// is non-sensical and must short-circuit.
func TestActiveSnapshotReferencesRefEmptyRef(t *testing.T) {
	t.Parallel()
	c := &catalog.Catalog{Ops: []catalog.Op{{OpID: "x"}}}
	if activeSnapshotReferencesRef(c, "") {
		t.Error("got=true; want false for empty ref")
	}
}

// TestActiveSnapshotReferencesRefVariantRequestRefMatches pins the
// variant `b.RequestRef == ref → true` arm (schema_resource.go:176-178).
func TestActiveSnapshotReferencesRefVariantRequestRefMatches(t *testing.T) {
	t.Parallel()
	c := &catalog.Catalog{
		Ops: []catalog.Op{{
			OpID: "op1",
			Variants: []catalog.Variant{{
				VariantID: "v1",
				Binding:   &catalog.Binding{RequestRef: "#/types/Req"},
			}},
		}},
	}
	if !activeSnapshotReferencesRef(c, "#/types/Req") {
		t.Error("got=false; want true (variant RequestRef match)")
	}
}

// TestActiveSnapshotReferencesRefVariantResponseRefMatches mirrors the
// above for the b.ResponseRef alternation at schema_resource.go:176.
func TestActiveSnapshotReferencesRefVariantResponseRefMatches(t *testing.T) {
	t.Parallel()
	c := &catalog.Catalog{
		Ops: []catalog.Op{{
			OpID: "op1",
			Variants: []catalog.Variant{{
				VariantID: "v1",
				Binding:   &catalog.Binding{ResponseRef: "#/types/Resp"},
			}},
		}},
	}
	if !activeSnapshotReferencesRef(c, "#/types/Resp") {
		t.Error("got=false; want true (variant ResponseRef match)")
	}
}

// TestActiveSnapshotReferencesRefOpResponseRefMatches pins the
// op.ResponseRef early-return arm (schema_resource.go:171-173). The
// match is on the op (not a variant) so we MUST return true before
// even inspecting variants.
func TestActiveSnapshotReferencesRefOpResponseRefMatches(t *testing.T) {
	t.Parallel()
	c := &catalog.Catalog{
		Ops: []catalog.Op{{OpID: "op1", ResponseRef: "#/types/OpResp"}},
	}
	if !activeSnapshotReferencesRef(c, "#/types/OpResp") {
		t.Error("got=false; want true (op.ResponseRef match)")
	}
}

// TestActiveSnapshotReferencesRefNoMatchReturnsFalse pins the final
// fall-through `return false` arm (schema_resource.go:181). A catalog
// with ops/variants that don't reference `ref` must return false.
func TestActiveSnapshotReferencesRefNoMatchReturnsFalse(t *testing.T) {
	t.Parallel()
	c := &catalog.Catalog{
		Ops: []catalog.Op{{
			OpID:        "op1",
			ResponseRef: "#/types/Other",
			Variants: []catalog.Variant{{
				VariantID: "v1",
				Binding:   &catalog.Binding{RequestRef: "#/types/Other2"},
			}},
		}},
	}
	if activeSnapshotReferencesRef(c, "#/types/Missing") {
		t.Error("got=true; want false (no match anywhere)")
	}
}
