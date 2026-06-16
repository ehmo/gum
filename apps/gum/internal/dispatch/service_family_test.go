package dispatch_test

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestDispatcherServiceFamily exercises the public ServiceFamily resolver
// (spec §6.3 line 1171 — gum_parallel's 429 isolation calls this to partition
// rate-limit budgets). Three behaviors:
//   - Known op → returns the catalog ServiceFamily verbatim.
//   - Unknown op → returns "" (defense in depth; routing has already vetted).
//   - The dispatcher satisfies the ServiceFamilyResolver capability so the
//     caller-side type assertion in gum_parallel never panics.
func TestDispatcherServiceFamily(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test-service-family",
		Ops: []catalog.Op{
			{
				OpID:             "gmail.users.messages.list",
				OpSchemaVersion:  1,
				Service:          "gmail",
				ServiceFamily:    "workspace",
				DefaultVariantID: "v1",
				Variants: []catalog.Variant{{
					VariantID:     "v1",
					Stability:     catalog.StabilityStable,
					InterfaceKind: catalog.InterfaceKindDiscoveryREST,
					BackendKind:   catalog.BackendKindTypedRestSDK,
					Preferred:     true,
					RiskClass:     catalog.RiskClassRead,
					AuthStrategy:  catalog.AuthStrategyBYOOAuth,
				}},
			},
		},
	}

	disp := dispatch.NewDispatcher(cat, map[string]dispatch.Adapter{})

	resolver, ok := disp.(dispatch.ServiceFamilyResolver)
	if !ok {
		t.Fatalf("dispatcher does not satisfy ServiceFamilyResolver")
	}

	t.Run("known_op_returns_family", func(t *testing.T) {
		if got := resolver.ServiceFamily("gmail.users.messages.list"); got != "workspace" {
			t.Errorf("ServiceFamily(known) = %q, want workspace", got)
		}
	})

	t.Run("unknown_op_returns_empty", func(t *testing.T) {
		if got := resolver.ServiceFamily("does.not.exist"); got != "" {
			t.Errorf("ServiceFamily(unknown) = %q, want empty", got)
		}
	})
}
