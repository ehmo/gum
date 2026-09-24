package dispatch

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// A quarantined default_variant_id must not end the call. Spec §5.5 makes
// "active, non-quarantined" part of what a default is, and the
// docs/test-matrix.md default-variant lifecycle row says
// the default never selects a quarantined variant while an active executable
// alternative exists. Quarantine is runtime state (plugin-state.json), so a
// catalog that was valid when generated can hold a quarantined default hours
// later; refusing the op then takes down a healthy sibling with it.
//
// These tests build the catalog by hand rather than through Validate. §5.1
// forbids a quarantined default at generation time, so the fixture is a state
// the generator is not supposed to emit; the runtime still has to survive it,
// because plugin-state.json can quarantine a variant after the catalog ships.

// quarantinedDefaultOp returns an op whose default is quarantined and whose
// sibling is healthy. The two carry different risk classes so a risk gate that
// reads the wrong one is visible in the assertion rather than silent.
func quarantinedDefaultOp() catalog.Op {
	dead := makeRoutingVariant("fallthrough.dead", catalog.StabilityStable, catalog.InterfaceKindDiscoveryREST)
	dead.Quarantined = true
	dead.RiskClass = catalog.RiskClassRead

	live := makeRoutingVariant("fallthrough.live", catalog.StabilityBeta, catalog.InterfaceKindGRPC)
	live.RiskClass = catalog.RiskClassWrite

	return catalog.Op{
		OpID:             "fallthrough.op",
		OpSchemaVersion:  1,
		Title:            "Op whose default variant was quarantined after generation",
		Summary:          "Used by the quarantined-default fall-through tests.",
		DefaultVariantID: "fallthrough.dead",
		Variants:         []catalog.Variant{dead, live},
	}
}

func quarantinedDefaultDispatcher(op catalog.Op) *dispatcher {
	return &dispatcher{
		snapshot: &catalog.Catalog{Ops: []catalog.Op{op}},
		adapters: map[string]Adapter{},
	}
}

func TestResolveVariantFallsThroughQuarantinedDefault(t *testing.T) {
	d := quarantinedDefaultDispatcher(quarantinedDefaultOp())

	rv, serr := d.resolveVariant(context.Background(), &Invocation{OpID: "fallthrough.op"})
	if serr != nil {
		t.Fatalf("resolveVariant = %s: %s; a healthy sibling exists, so the quarantined default must not end the call", serr.ErrCode, serr.Message)
	}
	if rv.Variant.VariantID != "fallthrough.live" {
		t.Errorf("resolved %q; want fallthrough.live, the only non-quarantined variant", rv.Variant.VariantID)
	}
}

// The risk gate must evaluate the variant that actually executes. If
// policyVariant kept returning the quarantined default, an op could clear a
// read-class gate and then run the write-class sibling.
func TestPolicyVariantSkipsQuarantinedDefault(t *testing.T) {
	d := quarantinedDefaultDispatcher(quarantinedDefaultOp())
	inv := &Invocation{OpID: "fallthrough.op"}

	gated := d.policyVariant(inv)
	if gated == nil {
		t.Fatal("policyVariant = nil; a healthy sibling is executable, so the gate must have a variant to read")
	}
	rv, serr := d.resolveVariant(context.Background(), inv)
	if serr != nil {
		t.Fatalf("resolveVariant = %s: %s", serr.ErrCode, serr.Message)
	}
	if gated.VariantID != rv.Variant.VariantID {
		t.Errorf("gate reads %q but dispatch runs %q; the risk gate must evaluate the executed variant", gated.VariantID, rv.Variant.VariantID)
	}
	if gated.RiskClass != catalog.RiskClassWrite {
		t.Errorf("gate reads risk_class %q; the executed sibling is write", gated.RiskClass)
	}
}

// The fall-through must not swallow the terminal case: with every variant
// quarantined there is nothing to fall through to.
func TestResolveVariantQuarantinedDefaultWithNoHealthySibling(t *testing.T) {
	op := quarantinedDefaultOp()
	op.Variants[1].Quarantined = true
	d := quarantinedDefaultDispatcher(op)

	rv, serr := d.resolveVariant(context.Background(), &Invocation{OpID: "fallthrough.op"})
	if serr == nil {
		t.Fatalf("resolveVariant succeeded with %q; every variant is quarantined", rv.Variant.VariantID)
	}
	if serr.ErrCode != ErrCodeVariantQuarantined {
		t.Errorf("ErrCode = %q; want %q", serr.ErrCode, ErrCodeVariantQuarantined)
	}
	if serr.Detail["variant_id"] != "fallthrough.dead" {
		t.Errorf("detail[variant_id] = %v; want the quarantined default the caller asked for", serr.Detail["variant_id"])
	}
}
