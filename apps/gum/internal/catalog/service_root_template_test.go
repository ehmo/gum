package catalog_test

// Tests for SERVICE_ROOT_TEMPLATE_DEFERRED rejection (spec.md §error-table,
// catalog-abi.md "Service Root Extension Point").
//
// These tests are RED until Green adds:
//   - catalog.Variant.ServiceRootTemplate string field
//   - catalog.ErrServiceRootTemplateDeferred sentinel
//   - rejection logic inside (*Op).Validate()

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// minimalValidCatalog returns a Catalog with exactly one Op and one Variant
// that passes all existing validation rules. The caller can then set
// v.ServiceRootTemplate before calling Validate().
func minimalValidCatalog() *catalog.Catalog {
	variantID := "test.v1.rest.foo.bar"
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "gum/cmd/gen-catalog@test",
		Ops: []catalog.Op{
			{
				OpID:             "test.foo.bar",
				OpSchemaVersion:  1,
				Title:            "Test operation",
				Summary:          "Minimal op for service_root_template tests.",
				DefaultVariantID: variantID,
				Variants: []catalog.Variant{
					{
						VariantID:            variantID,
						VariantSchemaVersion: 1,
						Stability:            catalog.StabilityStable,
						InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
						BackendKind:          catalog.BackendKindTypedRestSDK,
						RiskClass:            catalog.RiskClassRead,
						Binding: &catalog.Binding{
							BindingSchemaVersion: 1,
							AdapterKey:           "rest.typed-rest-sdk",
							OperationKey:         "test.v1.rest.foo.bar",
						},
					},
				},
			},
		},
	}
}

// TestServiceRootTemplateDeferred verifies that any non-empty
// service_root_template value is rejected with ErrServiceRootTemplateDeferred.
func TestServiceRootTemplateDeferred(t *testing.T) {
	const opID = "test.foo.bar"
	const variantID = "test.v1.rest.foo.bar"

	cases := []struct {
		name                string
		serviceRootTemplate string
		wantNilErr          bool
	}{
		{
			name:                "empty_template_passes",
			serviceRootTemplate: "",
			wantNilErr:          true,
		},
		{
			name:                "v04_form_rejected",
			serviceRootTemplate: "https://gmail.{universe_domain}",
			wantNilErr:          false,
		},
		{
			name:                "sovereign_host_rejected",
			serviceRootTemplate: "https://gmail.googleapis.us",
			wantNilErr:          false,
		},
		{
			name:                "placeholder_only_rejected",
			serviceRootTemplate: "{universe_domain}",
			wantNilErr:          false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat := minimalValidCatalog()
			cat.Ops[0].Variants[0].ServiceRootTemplate = tc.serviceRootTemplate

			err := cat.Validate()

			if tc.wantNilErr {
				if err != nil {
					t.Fatalf("Validate() expected nil error for empty service_root_template, got: %v", err)
				}
				return
			}

			// Failing cases: must get a non-nil error wrapping ErrServiceRootTemplateDeferred.
			if err == nil {
				t.Fatalf("Validate() expected non-nil error for service_root_template=%q, got nil", tc.serviceRootTemplate)
			}
			if !errors.Is(err, catalog.ErrServiceRootTemplateDeferred) {
				t.Fatalf("expected errors.Is(err, ErrServiceRootTemplateDeferred), got: %v", err)
			}
			// Error message must contain op_id and variant_id for CI triage.
			if !strings.Contains(err.Error(), opID) {
				t.Errorf("error message should contain op_id %q; got: %v", opID, err)
			}
			if !strings.Contains(err.Error(), variantID) {
				t.Errorf("error message should contain variant_id %q; got: %v", variantID, err)
			}
		})
	}
}
