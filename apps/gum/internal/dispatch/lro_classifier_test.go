package dispatch_test

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestDispatcherReturnsLRO exercises the public LROClassifier capability the
// §6.1 code-mode gate reads. The classification is the default variant's
// `lro_return` atom: a non-default variant that carries the atom does not make
// the op an LRO op, because gum_call dispatches the default.
func TestDispatcherReturnsLRO(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test-lro-classifier",
		Ops: []catalog.Op{
			{
				OpID:             "cloudidentity.groups.create",
				OpSchemaVersion:  1,
				Service:          "cloudidentity",
				DefaultVariantID: "v1",
				Variants: []catalog.Variant{{
					VariantID:     "v1",
					Stability:     catalog.StabilityStable,
					InterfaceKind: catalog.InterfaceKindDiscoveryREST,
					BackendKind:   catalog.BackendKindTypedRestSDK,
					RiskClass:     catalog.RiskClassWrite,
					AuthStrategy:  catalog.AuthStrategyBYOOAuth,
					Capabilities:  []string{catalog.CapabilityLROReturn},
				}},
			},
			{
				OpID:             "gmail.users.messages.list",
				OpSchemaVersion:  1,
				Service:          "gmail",
				DefaultVariantID: "v1",
				Variants: []catalog.Variant{
					{
						VariantID:     "v1",
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindDiscoveryREST,
						BackendKind:   catalog.BackendKindTypedRestSDK,
						RiskClass:     catalog.RiskClassRead,
						AuthStrategy:  catalog.AuthStrategyBYOOAuth,
					},
					{
						VariantID:     "v2",
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindDiscoveryREST,
						BackendKind:   catalog.BackendKindTypedRestSDK,
						RiskClass:     catalog.RiskClassRead,
						AuthStrategy:  catalog.AuthStrategyBYOOAuth,
						Capabilities:  []string{catalog.CapabilityLROReturn},
					},
				},
			},
		},
	}

	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{})
	classifier, ok := disp.(dispatch.LROClassifier)
	if !ok {
		t.Fatal("dispatcher does not satisfy LROClassifier")
	}

	cases := []struct {
		name string
		opID string
		want bool
	}{
		{"default_variant_declares_the_atom", "cloudidentity.groups.create", true},
		{"only_a_non_default_variant_declares_it", "gmail.users.messages.list", false},
		{"unknown_op", "does.not.exist", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifier.ReturnsLRO(tc.opID); got != tc.want {
				t.Errorf("ReturnsLRO(%q) = %v; want %v", tc.opID, got, tc.want)
			}
		})
	}
}

// TestDefaultVariantReturnsLROWithADanglingDefault covers the catalog helper's
// defensive arm: Op.Validate rejects a default_variant_id that names no
// variant, so only a hand-built Op can reach it.
func TestDefaultVariantReturnsLROWithADanglingDefault(t *testing.T) {
	op := &catalog.Op{
		OpID:             "fixture.op",
		DefaultVariantID: "missing",
		Variants: []catalog.Variant{{
			VariantID:    "v1",
			Capabilities: []string{catalog.CapabilityLROReturn},
		}},
	}
	if op.DefaultVariantReturnsLRO() {
		t.Fatal("DefaultVariantReturnsLRO() = true; want false for a dangling default_variant_id")
	}
}
