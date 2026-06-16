package dispatch_test

import (
	"context"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

func adminFixtureOwnershipCatalog() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test-admin-fixture-ownership",
		Ops: []catalog.Op{
			{
				OpID:             "admin.directory.users.update",
				OpSchemaVersion:  1,
				Title:            "Update Admin user",
				Summary:          "Fixture ownership dispatch test.",
				Service:          "admin",
				ServiceFamily:    "workspace",
				DefaultVariantID: "admin.directory.users.update.v1",
				Variants: []catalog.Variant{
					{
						VariantID:     "admin.directory.users.update.v1",
						Stability:     catalog.StabilityStable,
						InterfaceKind: catalog.InterfaceKindSDKNative,
						BackendKind:   catalog.BackendKindTypedRestSDK,
						RiskClass:     catalog.RiskClassWrite,
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           "test.counting",
							OperationKey:         "admin.directory.users.update",
						},
						AdminPolicy: &catalog.AdminPolicy{
							BlastRadius:              catalog.AdminBlastRadiusFixtureWrite,
							FixtureOwnershipRequired: true,
							FixtureMarkerPrefix:      catalog.AdminFixtureMarkerPrefix,
							FixtureResourceKeys:      []string{"userKey"},
						},
					},
				},
			},
		},
	}
}

func TestAdminFixtureOwnershipRejectsNonFixtureBeforeAdapter(t *testing.T) {
	adapter := &confirmCountingAdapter{}
	disp := dispatch.NewDispatcher(adminFixtureOwnershipCatalog(), map[string]dispatch.Adapter{"test.counting": adapter})

	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:       "admin.directory.users.update",
		Args:       map[string]any{"userKey": "alice@example.com"},
		Format:     "json",
		RequestID:  "test-admin-fixture-reject",
		AllowWrite: true,
	})
	if err == nil {
		t.Fatal("Dispatch(non-fixture Admin write) succeeded; want POLICY_DENIED")
	}
	se, ok := err.(*dispatch.StructuredError)
	if !ok {
		t.Fatalf("Dispatch(non-fixture Admin write) err = %T; want *StructuredError", err)
	}
	if se.ErrCode != dispatch.ErrCodePolicyDenied {
		t.Fatalf("ErrCode = %q, want %q", se.ErrCode, dispatch.ErrCodePolicyDenied)
	}
	if se.Detail["op_id"] != "admin.directory.users.update" {
		t.Fatalf("op_id detail = %v, want admin.directory.users.update", se.Detail["op_id"])
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter calls = %d, want 0", adapter.calls)
	}
}

func TestAdminFixtureOwnershipAllowsFixtureResource(t *testing.T) {
	adapter := &confirmCountingAdapter{}
	disp := dispatch.NewDispatcher(adminFixtureOwnershipCatalog(), map[string]dispatch.Adapter{"test.counting": adapter})

	_, err := disp.Dispatch(context.Background(), &dispatch.Invocation{
		OpID:       "admin.directory.users.update",
		Args:       map[string]any{"userKey": "gum-fixture-user@example.com"},
		Format:     "json",
		RequestID:  "test-admin-fixture-allow",
		AllowWrite: true,
	})
	if err != nil {
		t.Fatalf("Dispatch(fixture Admin write) = %v, want nil", err)
	}
	if adapter.calls != 1 {
		t.Fatalf("adapter calls = %d, want 1", adapter.calls)
	}
}
