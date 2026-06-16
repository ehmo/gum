package dispatch

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestFindOpVariantUnknownOpReturnsNil pins the `op == nil → return nil`
// arm: dispatcher.findOpVariant is called from Dispatch BEFORE
// resolveVariant has a chance to raise OP_NOT_FOUND, so an unknown opID
// MUST NOT panic — the defensive nil return is what lets the caller
// fall through to the proper error envelope downstream.
func TestFindOpVariantUnknownOpReturnsNil(t *testing.T) {
	d := &dispatcher{
		snapshot: &catalog.Catalog{
			CatalogSchemaVersion: 1,
			Ops:                  []catalog.Op{},
		},
		adapters: map[string]Adapter{},
	}
	if got := d.findOpVariant("does.not.exist"); got != nil {
		t.Errorf("findOpVariant(unknown) = %+v; want nil", got)
	}
}

// TestFindOpVariantDefaultIDMismatchReturnsNil pins the
// `loop-exhausted-without-match → return nil` arm: an op whose
// DefaultVariantID does NOT exist in its Variants slice MUST surface as
// nil rather than the first variant or a panic. This shape only happens
// when a catalog passes lax validation (e.g. routing tests bypass
// Catalog.Validate) but the safety net here matters for runtime
// resilience.
func TestFindOpVariantDefaultIDMismatchReturnsNil(t *testing.T) {
	d := &dispatcher{
		snapshot: &catalog.Catalog{
			CatalogSchemaVersion: 1,
			Ops: []catalog.Op{
				{
					OpID:             "missing.default",
					OpSchemaVersion:  1,
					DefaultVariantID: "v.that.does.not.exist",
					Variants: []catalog.Variant{
						{VariantID: "v.real.1", Stability: catalog.StabilityStable},
						{VariantID: "v.real.2", Stability: catalog.StabilityStable},
					},
				},
			},
		},
		adapters: map[string]Adapter{},
	}
	if got := d.findOpVariant("missing.default"); got != nil {
		t.Errorf("findOpVariant(default-mismatch) = %+v; want nil", got)
	}
}

// TestStabilityRankUnknownStabilityReturns3 pins the `default → 3` arm:
// any Stability value outside {stable, beta, alpha} MUST sort LAST in
// stability comparisons (rank 3 = worst). This is what protects routing
// from a future catalog that adds a new stability tier the dispatcher
// hasn't been taught yet.
func TestStabilityRankUnknownStabilityReturns3(t *testing.T) {
	if got := stabilityRank(catalog.Stability("future-tier")); got != 3 {
		t.Errorf("stabilityRank(unknown) = %d; want 3 (default)", got)
	}
	if got := stabilityRank(catalog.Stability("")); got != 3 {
		t.Errorf("stabilityRank(\"\") = %d; want 3 (default)", got)
	}
}
