package catalog_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestCatalogValidateRejectsUnparseableGeneratedAt pins the
// `time.Parse(RFC3339, GeneratedAt) err → ErrMissingRequiredField` arm
// of Catalog.Validate. A catalog with a non-empty but malformed
// generated_at (e.g. "yesterday" rather than RFC3339) MUST be rejected
// at load time; build-time gen-catalog stamps RFC3339, and accepting
// anything else would erode reproducible-build provenance.
func TestCatalogValidateRejectsUnparseableGeneratedAt(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.GeneratedAt = "definitely not rfc3339"
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate(unparseable generated_at)=nil; want ErrMissingRequiredField")
	}
	if !errors.Is(err, catalog.ErrMissingRequiredField) {
		t.Fatalf("Validate()=%v; want ErrMissingRequiredField", err)
	}
}

// TestCatalogValidateRejectsEmptyGeneratorVersion pins the
// `GeneratorVersion == "" → ErrMissingRequiredField` arm. Required for
// catalog-ABI bookkeeping (spec §5.3); without it, downstream rollback
// tooling can't identify which generator produced the catalog.
func TestCatalogValidateRejectsEmptyGeneratorVersion(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.GeneratorVersion = ""
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate(empty generator_version)=nil; want ErrMissingRequiredField")
	}
	if !errors.Is(err, catalog.ErrMissingRequiredField) {
		t.Fatalf("Validate()=%v; want ErrMissingRequiredField", err)
	}
}

// TestOpValidateMissingRequiredFieldBranches covers the per-field
// ErrMissingRequiredField arms of Op.Validate not exercised by the
// existing catalog-fixture suite. Each row mutates the loaded sample
// catalog to violate exactly one required-field invariant; Op.Validate
// MUST surface a wrapped ErrMissingRequiredField for every one.
//
// These arms exist because Op.Validate is the catalog-load gatekeeper
// (build-time `gum gen-catalog` and runtime catalog-snapshot reload
// both run it); a missing required field that slips through here would
// blow up downstream dispatch with confusing nil-deref / "" lookups.
func TestOpValidateMissingRequiredFieldBranches(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*catalog.Catalog)
	}{
		{
			name:   "zero op_schema_version",
			mutate: func(c *catalog.Catalog) { c.Ops[0].OpSchemaVersion = 0 },
		},
		{
			name:   "empty title",
			mutate: func(c *catalog.Catalog) { c.Ops[0].Title = "" },
		},
		{
			name:   "empty summary",
			mutate: func(c *catalog.Catalog) { c.Ops[0].Summary = "" },
		},
		{
			name:   "zero variant_schema_version",
			mutate: func(c *catalog.Catalog) { c.Ops[0].Variants[0].VariantSchemaVersion = 0 },
		},
		{
			name: "empty variants slice",
			mutate: func(c *catalog.Catalog) {
				// Keep DefaultVariantID non-empty so the earlier
				// `DefaultVariantID == ""` guard doesn't fire first.
				// Order in Op.Validate: (5) default-non-empty →
				// (6) variants-non-empty → (7) dangling-default. We
				// want to land on (6), which means leaving (5) intact
				// and clearing variants entirely.
				c.Ops[0].Variants = nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := loadFixture(t, "sample-catalog.json")
			tc.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("Validate()=nil for %s; want ErrMissingRequiredField", tc.name)
			}
			if !errors.Is(err, catalog.ErrMissingRequiredField) {
				t.Fatalf("Validate()=%v; want ErrMissingRequiredField wrap", err)
			}
		})
	}
}

// TestOpValidateMissingRequiredFieldVariantsTriggersEmptyVariantsArm
// pins specifically the `len(op.Variants) == 0` guard. The case above
// already exercises it, but this one keeps the assertion focused so a
// future contributor cannot accidentally regress just the empty-slice
// arm while keeping the table green via the other rows.
func TestOpValidateMissingRequiredFieldVariantsTriggersEmptyVariantsArm(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	// DefaultVariantID intentionally left intact; otherwise the
	// earlier `DefaultVariantID == ""` guard fires before we reach
	// the empty-variants check.
	c.Ops[0].Variants = []catalog.Variant{}
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate(no variants)=nil; want ErrMissingRequiredField")
	}
	if !errors.Is(err, catalog.ErrMissingRequiredField) {
		t.Fatalf("Validate(no variants)=%v; want ErrMissingRequiredField", err)
	}
}

// TestOpValidateRejectsUnknownStability pins the
// `!v.Stability.Valid() → ErrUnknownStability` arm. Spec §5.3 fixes the
// stability enum (alpha|beta|ga|deprecated); a typo would silently
// reorder priority in stabilityRank — pinning prevents downstream
// dispatch from accepting an unrecognised tier.
func TestOpValidateRejectsUnknownStability(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.Ops[0].Variants[0].Stability = catalog.Stability("future-tier")
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate(unknown stability)=nil; want ErrUnknownStability")
	}
	if !errors.Is(err, catalog.ErrUnknownStability) {
		t.Fatalf("Validate()=%v; want ErrUnknownStability", err)
	}
}

// TestOpValidateRejectsUnknownInterfaceKind pins the
// `!v.InterfaceKind.Valid() → ErrUnknownInterfaceKind` arm. The
// interface-kind enum drives which adapter handles the variant; a
// silently accepted typo would crash dispatch at adapter-lookup time.
func TestOpValidateRejectsUnknownInterfaceKind(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.Ops[0].Variants[0].InterfaceKind = catalog.InterfaceKind("teleport")
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate(unknown interface_kind)=nil; want ErrUnknownInterfaceKind")
	}
	if !errors.Is(err, catalog.ErrUnknownInterfaceKind) {
		t.Fatalf("Validate()=%v; want ErrUnknownInterfaceKind", err)
	}
}

// TestOpValidateRejectsEmptyVariantID pins the per-variant
// `v.VariantID == "" → ErrMissingRequiredField` arm. The existing
// catalog-fixture suite tries to exercise this by clearing
// Variants[0].VariantID, but that mutation actually trips the earlier
// ErrDanglingDefaultVariantID guard first (DefaultVariantID can no
// longer be found among the remaining variants). To reach the empty-
// variant-ID guard we keep Variants[0] intact (so the default still
// resolves) and append a second variant with an empty ID — the
// per-variant loop then hits the empty-ID branch on iteration 2.
func TestOpValidateRejectsEmptyVariantID(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	good := c.Ops[0].Variants[0] // keep DefaultVariantID resolvable
	c.Ops[0].Variants = []catalog.Variant{good, {VariantID: ""}}
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate(empty variant_id)=nil; want ErrMissingRequiredField")
	}
	if !errors.Is(err, catalog.ErrMissingRequiredField) {
		t.Fatalf("Validate()=%v; want ErrMissingRequiredField", err)
	}
}

// TestOpValidateRejectsUnknownBackendKind pins the
// `!v.BackendKind.Valid() → ErrUnknownBackendKind` arm. Symmetric guard
// to interface-kind; mis-typed backend would route to a non-existent
// executor at dispatch time.
func TestOpValidateRejectsUnknownBackendKind(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.Ops[0].Variants[0].BackendKind = catalog.BackendKind("quantum")
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate(unknown backend_kind)=nil; want ErrUnknownBackendKind")
	}
	if !errors.Is(err, catalog.ErrUnknownBackendKind) {
		t.Fatalf("Validate()=%v; want ErrUnknownBackendKind", err)
	}
}
