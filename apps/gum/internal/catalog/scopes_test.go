package catalog_test

import (
	"reflect"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// scopeCatalog builds a tiny in-memory catalog with known scopes so the
// derivation helpers can be asserted without depending on the embedded data.
func scopeCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{
			{
				OpID:             "sc.query",
				DefaultVariantID: "sc.query.v1",
				Variants: []catalog.Variant{
					{VariantID: "sc.query.v1", Scopes: []string{"https://www.googleapis.com/auth/webmasters.readonly"}},
					{VariantID: "sc.query.v2", Scopes: []string{"https://www.googleapis.com/auth/webmasters"}},
				},
			},
			{
				OpID:             "drive.list",
				DefaultVariantID: "drive.list.v1",
				Variants: []catalog.Variant{
					// Default variant deliberately shares a scope with sc.query.v2
					// so AllScopes can be shown to dedupe.
					{VariantID: "drive.list.v1", Scopes: []string{"https://www.googleapis.com/auth/webmasters", "https://www.googleapis.com/auth/drive.readonly"}},
				},
			},
			{
				OpID:             "plugin.thing",
				DefaultVariantID: "plugin.thing.v1",
				Variants: []catalog.Variant{
					{VariantID: "plugin.thing.v1"}, // no scopes (plugin-managed)
				},
			},
		},
	}
}

// TestScopesForOpReturnsDefaultVariantScopes verifies the helper returns the
// scopes declared on the op's default variant — the set the dispatch kernel
// actually requests — not those of a non-default variant.
func TestScopesForOpReturnsDefaultVariantScopes(t *testing.T) {
	c := scopeCatalog()
	got := c.ScopesForOp("sc.query")
	want := []string{"https://www.googleapis.com/auth/webmasters.readonly"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ScopesForOp(sc.query) = %v, want %v", got, want)
	}
}

// TestScopesForOpReturnsCopy verifies callers cannot mutate catalog-owned
// scope slices through the helper result.
func TestScopesForOpReturnsCopy(t *testing.T) {
	c := scopeCatalog()
	got := c.ScopesForOp("sc.query")
	got[0] = "mutated"
	again := c.ScopesForOp("sc.query")
	if again[0] == "mutated" {
		t.Fatalf("ScopesForOp returned catalog-owned slice; second read = %v", again)
	}
}

// TestScopesForOpUnknownOpReturnsNil verifies an unknown op id yields nil so
// callers can treat "no auth needed" and "op not found" uniformly (the dispatch
// path validates op existence separately).
func TestScopesForOpUnknownOpReturnsNil(t *testing.T) {
	if got := scopeCatalog().ScopesForOp("does.not.exist"); got != nil {
		t.Fatalf("ScopesForOp(unknown) = %v, want nil", got)
	}
}

// TestScopesForOpMissingDefaultVariantReturnsNil verifies a stale
// default_variant_id does not accidentally fall back to another variant.
func TestScopesForOpMissingDefaultVariantReturnsNil(t *testing.T) {
	c := &catalog.Catalog{Ops: []catalog.Op{{
		OpID:             "stale.default",
		DefaultVariantID: "missing",
		Variants:         []catalog.Variant{{VariantID: "present", Scopes: []string{"scope"}}},
	}}}
	if got := c.ScopesForOp("stale.default"); got != nil {
		t.Fatalf("ScopesForOp(stale.default) = %v, want nil", got)
	}
}

// TestScopesForOpScopelessVariantReturnsEmpty verifies a plugin-managed op whose
// default variant declares no scopes returns no scopes (nothing to authorize).
func TestScopesForOpScopelessVariantReturnsEmpty(t *testing.T) {
	if got := scopeCatalog().ScopesForOp("plugin.thing"); len(got) != 0 {
		t.Fatalf("ScopesForOp(plugin.thing) = %v, want empty", got)
	}
}

// TestAllScopesReturnsSortedUniqueUnion verifies AllScopes collects every scope
// across all ops and variants, de-duplicated and sorted, so `gum login` can
// request the full set in a single consent screen.
func TestAllScopesReturnsSortedUniqueUnion(t *testing.T) {
	got := scopeCatalog().AllScopes()
	want := []string{
		"https://www.googleapis.com/auth/drive.readonly",
		"https://www.googleapis.com/auth/webmasters",
		"https://www.googleapis.com/auth/webmasters.readonly",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AllScopes() = %v, want %v", got, want)
	}
}

// TestAllScopesEmptyCatalog verifies an empty catalog yields nil rather than a
// non-nil empty slice, so callers can short-circuit on == nil.
func TestAllScopesEmptyCatalog(t *testing.T) {
	if got := (&catalog.Catalog{}).AllScopes(); got != nil {
		t.Fatalf("AllScopes(empty) = %v, want nil", got)
	}
}
