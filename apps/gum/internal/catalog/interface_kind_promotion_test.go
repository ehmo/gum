// Fixture-backed contract test for the `interface_kind` extension procedure
// in docs/catalog-abi.md ("Interface Kind"). The procedure is a four-step PR
// sequence, and step 3 requires a fixture-backed contract test. The clause
// that most needs one is the closing sentence: "Catalog rebuilds during the
// promotion window MUST treat the experimental `x-<name>` and the stable
// `<name>` as distinct values; the migration is not silent."
//
// The fixture models step 1: one op whose only variant ships the
// experimental kind `x-sdk-native` under `execution_support: "schema_only"`.
// Each subtest advances it one step, so a regression names the step it broke.

package catalog_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

const (
	promotionExperimentalKind = catalog.InterfaceKind("x-sdk-native")
	promotionStableKind       = catalog.InterfaceKindSDKNative
	promotionVariantID        = "labs.v1.experimental.widgets.render"
)

// loadPromotionFixture returns the step-1 catalog: experimental kind,
// schema-only support. It fails the test if the fixture itself has drifted
// out of that state, because every subtest reasons from it.
func loadPromotionFixture(t *testing.T) *catalog.Catalog {
	t.Helper()
	c := loadFixture(t, "interface-kind-promotion.json")
	v := c.Ops[0].Variants[0]
	if v.VariantID != promotionVariantID {
		t.Fatalf("fixture variant_id = %q; want %q", v.VariantID, promotionVariantID)
	}
	if v.InterfaceKind != promotionExperimentalKind {
		t.Fatalf("fixture interface_kind = %q; want %q", v.InterfaceKind, promotionExperimentalKind)
	}
	if v.ExecutionSupport != catalog.ExecutionSupportSchemaOnly {
		t.Fatalf("fixture execution_support = %q; want %q", v.ExecutionSupport, catalog.ExecutionSupportSchemaOnly)
	}
	return c
}

func TestInterfaceKindPromotionFixture(t *testing.T) {
	// Step 1: the experimental value lands under schema_only and loads.
	t.Run("experimental_kind_loads_schema_only", func(t *testing.T) {
		c := loadPromotionFixture(t)
		if err := c.Validate(); err != nil {
			t.Fatalf("Validate(step 1 fixture) = %v; want nil: an x-* schema_only variant is loadable", err)
		}
		if !promotionExperimentalKind.Valid() {
			t.Errorf("(%q).Valid() = false; the x- escape hatch must accept the experimental kind", promotionExperimentalKind)
		}
	})

	// Step 3, done wrong: dropping the `x-` prefix without registering the
	// value in the closed-enum validator. The table edit alone must not make
	// the kind loadable, or the promotion window would be silent.
	t.Run("bare_unregistered_name_is_rejected", func(t *testing.T) {
		c := loadPromotionFixture(t)
		c.Ops[0].Variants[0].InterfaceKind = catalog.InterfaceKind("widget-rpc")
		err := c.Validate()
		if err == nil {
			t.Fatal("Validate(unregistered promoted kind) = nil; want ErrUnknownInterfaceKind")
		}
		if !errors.Is(err, catalog.ErrUnknownInterfaceKind) {
			t.Fatalf("Validate() = %v; want ErrUnknownInterfaceKind", err)
		}
	})

	// Promotion window: both values coexist in one rebuild and stay distinct.
	// A loader that normalized `x-sdk-native` to `sdk-native` would leave one
	// execution_support behind and make the experimental variant executable.
	t.Run("promotion_window_keeps_both_values_distinct", func(t *testing.T) {
		c := loadPromotionFixture(t)
		promoted := c.Ops[0].Variants[0]
		promoted.VariantID = "labs.v1.sdk.widgets.render"
		promoted.InterfaceKind = promotionStableKind
		promoted.Stability = catalog.StabilityStable
		promoted.ExecutionSupport = catalog.ExecutionSupportFull
		promoted.UnsupportedCapabilities = nil
		promoted.Preferred = false
		c.Ops[0].Variants = append(c.Ops[0].Variants, promoted)

		if err := c.Validate(); err != nil {
			t.Fatalf("Validate(promotion window) = %v; want nil", err)
		}

		got := c.Ops[0].Variants
		if len(got) != 2 {
			t.Fatalf("variants = %d; want 2", len(got))
		}
		if got[0].InterfaceKind != promotionExperimentalKind {
			t.Errorf("experimental variant interface_kind = %q; want %q (migration must not be silent)",
				got[0].InterfaceKind, promotionExperimentalKind)
		}
		if got[1].InterfaceKind != promotionStableKind {
			t.Errorf("promoted variant interface_kind = %q; want %q", got[1].InterfaceKind, promotionStableKind)
		}
		if got[0].ExecutionSupport != catalog.ExecutionSupportSchemaOnly {
			t.Errorf("experimental variant execution_support = %q; want %q",
				got[0].ExecutionSupport, catalog.ExecutionSupportSchemaOnly)
		}
		if got[1].ExecutionSupport != catalog.ExecutionSupportFull {
			t.Errorf("promoted variant execution_support = %q; want %q",
				got[1].ExecutionSupport, catalog.ExecutionSupportFull)
		}

		// The distinction survives a rebuild, which is what "catalog rebuilds
		// MUST treat them as distinct" means in wire terms.
		raw := mustMarshal(t, c)
		var round catalog.Catalog
		if err := json.Unmarshal(raw, &round); err != nil {
			t.Fatalf("round-trip unmarshal: %v", err)
		}
		if round.Ops[0].Variants[0].InterfaceKind == round.Ops[0].Variants[1].InterfaceKind {
			t.Errorf("round-trip collapsed both kinds to %q", round.Ops[0].Variants[0].InterfaceKind)
		}
	})

	// Step 3, completed: the record flips to the stable kind and to "full".
	t.Run("promoted_variant_flips_to_full", func(t *testing.T) {
		c := loadPromotionFixture(t)
		v := &c.Ops[0].Variants[0]
		v.InterfaceKind = promotionStableKind
		v.Stability = catalog.StabilityStable
		v.ExecutionSupport = catalog.ExecutionSupportFull
		v.UnsupportedCapabilities = nil
		if err := c.Validate(); err != nil {
			t.Fatalf("Validate(promoted variant) = %v; want nil", err)
		}
	})

	// The flip's live hazard: keeping the schema-only capability list after
	// setting "full" leaves describe_op advertising blocked atoms on an
	// executable variant.
	t.Run("promoted_variant_must_drop_unsupported_capabilities", func(t *testing.T) {
		c := loadPromotionFixture(t)
		v := &c.Ops[0].Variants[0]
		v.InterfaceKind = promotionStableKind
		v.Stability = catalog.StabilityStable
		v.ExecutionSupport = catalog.ExecutionSupportFull
		err := c.Validate()
		if err == nil {
			t.Fatal("Validate(full + stale unsupported_capabilities) = nil; want ErrUnexpectedUnsupportedCapabilities")
		}
		if !errors.Is(err, catalog.ErrUnexpectedUnsupportedCapabilities) {
			t.Fatalf("Validate() = %v; want ErrUnexpectedUnsupportedCapabilities", err)
		}
	})
}
