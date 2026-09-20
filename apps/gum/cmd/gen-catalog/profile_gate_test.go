package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
	"github.com/ehmo/gum/internal/output/profile"
)

// TestEmbeddedCatalogPassesTheBuildProfileGate is the regression guard for
// gum-36f5: the shipped catalog must not carry a variant whose output_profile
// resolves to nothing. flights.v1.plugin.search named "flights.search.v1", a
// profile that has never existed in internal/output/profile/builtin, so the
// variant dispatched with no profile while the catalog claimed one.
func TestEmbeddedCatalogPassesTheBuildProfileGate(t *testing.T) {
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("decode embedded catalog: %v", err)
	}
	if len(cat.Ops) == 0 {
		t.Fatal("embedded catalog has no ops; the gate would pass vacuously")
	}

	if err := validateVariantProfiles(&cat, profile.BuiltinLookup); err != nil {
		t.Fatalf("shipped catalog fails the build profile gate: %v", err)
	}
}

// TestValidateVariantProfilesRejectsDanglingName pins the gate's own behaviour
// so a future dangling reference cannot slip through silently.
func TestValidateVariantProfilesRejectsDanglingName(t *testing.T) {
	cat := &catalog.Catalog{Ops: []catalog.Op{{
		OpID: "demo.op",
		Variants: []catalog.Variant{{
			VariantID:     "demo.v1",
			OutputProfile: "no.such.profile.v1",
		}},
	}}}

	err := validateVariantProfiles(cat, profile.BuiltinLookup)
	if !errors.Is(err, errOutputProfileNotFound) {
		t.Fatalf("validateVariantProfiles = %v; want errOutputProfileNotFound", err)
	}
	if !strings.Contains(err.Error(), "demo.v1") {
		t.Errorf("error %q does not name the offending variant", err)
	}
}

// TestValidateVariantProfilesRunsStripNullsSafety proves the gate carries the
// variant's null_elision_safe_fields into the bound check. docs/spec.md §7
// lists Build as a firing point for PROFILE_STRIP_NULLS_UNSAFE; before this
// gate the code had no build-time producer.
func TestValidateVariantProfilesRunsStripNullsSafety(t *testing.T) {
	const name = "gate.strip_nulls.v1"
	p, err := profile.Parse("strip_nulls = true\nkeep_fields = [\"a.b\"]\n")
	if err != nil {
		t.Fatalf("parse fixture profile: %v", err)
	}
	lookup := func(want string) (*profile.Profile, bool) {
		if want != name {
			return nil, false
		}
		return p, true
	}

	cat := &catalog.Catalog{Ops: []catalog.Op{{
		OpID: "demo.op",
		Variants: []catalog.Variant{{
			VariantID:     "demo.v1",
			OutputProfile: name,
		}},
	}}}

	if err := validateVariantProfiles(cat, lookup); !errors.Is(err, profile.ErrProfileStripNullsUnsafe) {
		t.Fatalf("validateVariantProfiles = %v; want ErrProfileStripNullsUnsafe", err)
	}

	cat.Ops[0].Variants[0].NullElisionSafeFields = []string{"a.b"}
	if err := validateVariantProfiles(cat, lookup); err != nil {
		t.Fatalf("declared safe field still rejected: %v", err)
	}
}

// TestValidateVariantProfilesSkipsUnsetNames keeps the gate off the 225 shipped
// variants that name no profile at all.
func TestValidateVariantProfilesSkipsUnsetNames(t *testing.T) {
	cat := &catalog.Catalog{Ops: []catalog.Op{{
		OpID:     "demo.op",
		Variants: []catalog.Variant{{VariantID: "demo.v1"}},
	}}}

	if err := validateVariantProfiles(cat, profile.BuiltinLookup); err != nil {
		t.Fatalf("variant without output_profile rejected: %v", err)
	}
}
