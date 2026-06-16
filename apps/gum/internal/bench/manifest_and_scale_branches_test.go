package bench_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/bench"
	"github.com/ehmo/gum/internal/catalog"
)

// TestCategoryRatiosEmptyTotalReturnsEmptyMap pins
// release_fixtures.go:56-58 — `m.Total == 0 → return out (empty map)`.
// Without this short-circuit, the ratio loop would divide by zero.
// The contract returns an empty (not nil) map so callers can compare
// to zero without a nil-deref handling step.
func TestCategoryRatiosEmptyTotalReturnsEmptyMap(t *testing.T) {
	t.Parallel()
	m := &bench.ReleaseManifest{Total: 0}
	got := m.CategoryRatios()
	if got == nil {
		t.Fatal("CategoryRatios()=nil; want empty map")
	}
	if len(got) != 0 {
		t.Errorf("len(got)=%d; want 0", len(got))
	}
}

// TestSpecScaleNaiveCatalogNilBaseReturnsError pins
// spec_scale_catalog.go:51-53 — `base == nil → error`. A nil base
// has no ops to clone and no metadata to seed the synthetic naive
// catalog, so the function MUST fail fast with a clear "nil base
// catalog" message rather than nil-deref deep inside the loop.
func TestSpecScaleNaiveCatalogNilBaseReturnsError(t *testing.T) {
	t.Parallel()
	_, err := bench.SpecScaleNaiveCatalog(nil, "")
	if err == nil {
		t.Fatal("SpecScaleNaiveCatalog(nil, \"\")=nil err; want nil-base err")
	}
	if !strings.Contains(err.Error(), "nil base catalog") {
		t.Errorf("err=%q; want 'nil base catalog' surface", err)
	}
}

// TestSpecScaleNaiveCatalogEmptyOpsReturnsError pins
// spec_scale_catalog.go:54-56 — `len(base.Ops) == 0 → error`. The
// scale algorithm clones base ops to pad to SpecScaleOpsTarget; an
// empty base means there is nothing to clone, so the function fails
// fast rather than producing an all-synthetic-zero catalog.
func TestSpecScaleNaiveCatalogEmptyOpsReturnsError(t *testing.T) {
	t.Parallel()
	base := &catalog.Catalog{Ops: nil}
	_, err := bench.SpecScaleNaiveCatalog(base, "")
	if err == nil {
		t.Fatal("SpecScaleNaiveCatalog(empty base)=nil err; want 'no ops' err")
	}
	if !strings.Contains(err.Error(), "no ops") {
		t.Errorf("err=%q; want 'no ops' surface", err)
	}
}

// TestSpecScaleNaiveCatalogScanFixturesErrorWraps pins
// spec_scale_catalog.go:67-69 — `collectFixtureOpIDs err →
// "scan fixtures:" wrap`. A nonexistent fixture directory forces
// the scan to fail; the wrap distinguishes this from the validate-
// base nil-check arms above.
func TestSpecScaleNaiveCatalogScanFixturesErrorWraps(t *testing.T) {
	t.Parallel()
	base := &catalog.Catalog{
		Ops: []catalog.Op{{OpID: "x.y.z"}},
	}
	_, err := bench.SpecScaleNaiveCatalog(base, "/tmp/definitely-not-a-fixture-dir-xyz123")
	if err == nil {
		t.Fatal("SpecScaleNaiveCatalog(bad fixture dir)=nil err; want scan-fixtures wrap")
	}
	if !strings.Contains(err.Error(), "scan fixtures:") {
		t.Errorf("err=%q; want 'scan fixtures:' wrap", err)
	}
}
