package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestResolveVariantHonorsRequestedVariantID verifies the spec §5.1 explicit
// variant pin path: when Invocation.RequestedVariantID is set, resolveVariant
// returns exactly that variant (skipping the default-variant policy chain).
func TestResolveVariantHonorsRequestedVariantID(t *testing.T) {
	d := &dispatcher{
		snapshot: variantPinCatalog(),
		adapters: map[string]Adapter{},
	}
	inv := &Invocation{
		OpID:               "pin.example.op",
		RequestedVariantID: "pin.example.op.v2.rest",
	}
	rv, serr := d.resolveVariant(context.Background(), inv)
	if serr != nil {
		t.Fatalf("resolveVariant: %v", serr)
	}
	if rv.Variant.VariantID != "pin.example.op.v2.rest" {
		t.Errorf("resolved variant_id = %s; want pin.example.op.v2.rest (pinned override of default v1)", rv.Variant.VariantID)
	}
}

// TestResolveVariantPinUnknownReturnsVariantNotFound verifies that a
// nonexistent pinned variant fails before any upstream call with the stable
// VARIANT_NOT_FOUND code per spec §12.0 + §1421.
func TestResolveVariantPinUnknownReturnsVariantNotFound(t *testing.T) {
	d := &dispatcher{
		snapshot: variantPinCatalog(),
		adapters: map[string]Adapter{},
	}
	_, serr := d.resolveVariant(context.Background(), &Invocation{
		OpID:               "pin.example.op",
		RequestedVariantID: "pin.example.op.does.not.exist",
	})
	if serr == nil || serr.ErrCode != ErrCodeVariantNotFound {
		t.Fatalf("expected VARIANT_NOT_FOUND, got %v", serr)
	}
	if serr.Detail["variant_id"] != "pin.example.op.does.not.exist" {
		t.Errorf("detail.variant_id missing/wrong: %#v", serr.Detail)
	}
}

// TestResolveVariantPinQuarantinedReturnsVariantQuarantined verifies that
// pinning a quarantined variant_id explicitly is still rejected (pin must
// not bypass quarantine policy).
func TestResolveVariantPinQuarantinedReturnsVariantQuarantined(t *testing.T) {
	d := &dispatcher{
		snapshot: variantPinCatalog(),
		adapters: map[string]Adapter{},
	}
	_, serr := d.resolveVariant(context.Background(), &Invocation{
		OpID:               "pin.example.op",
		RequestedVariantID: "pin.example.op.v3.quarantined",
	})
	if serr == nil || serr.ErrCode != ErrCodeVariantQuarantined {
		t.Fatalf("expected VARIANT_QUARANTINED, got %v", serr)
	}
}

// variantPinCatalog is a minimal catalog with three variants under one op:
//   - v1.rest (default)
//   - v2.rest (alternate, used by the pin test)
//   - v3.quarantined (rejected even when pinned)
func variantPinCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test@0.0.0",
		Ops: []catalog.Op{{
			OpID:             "pin.example.op",
			OpSchemaVersion:  1,
			DefaultVariantID: "pin.example.op.v1.rest",
			Variants: []catalog.Variant{
				makePinVariant("pin.example.op.v1.rest", false),
				makePinVariant("pin.example.op.v2.rest", false),
				makePinVariant("pin.example.op.v3.quarantined", true),
			},
		}},
	}
}

func makePinVariant(id string, quarantined bool) catalog.Variant {
	return catalog.Variant{
		VariantID:     id,
		Stability:     catalog.StabilityStable,
		InterfaceKind: catalog.InterfaceKindDiscoveryREST,
		BackendKind:   catalog.BackendKindTypedRestSDK,
		RiskClass:     catalog.RiskClassRead,
		Quarantined:   quarantined,
		Binding: &catalog.Binding{
			BindingSchemaVersion: 1,
			AdapterKey:           "test.adapter",
			OperationKey:         id + ".exec",
		},
	}
}
