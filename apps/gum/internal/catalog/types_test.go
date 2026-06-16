package catalog_test

import (
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// loadFixture reads testdata/<name> and unmarshals into a Catalog.
func loadFixture(t *testing.T, name string) *catalog.Catalog {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("loadFixture: read %s: %v", name, err)
	}
	var c catalog.Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("loadFixture: unmarshal %s: %v", name, err)
	}
	return &c
}

// mutate returns a deep copy of the fixture JSON with the given jq-style path set to value.
// For simplicity we use string replacement; production tests should use a proper JSON mutator.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustMarshal: %v", err)
	}
	return b
}

// TestCatalogValidateAcceptsValid loads the sample catalog fixture, parses it, and expects Validate() == nil.
func TestCatalogValidateAcceptsValid(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() returned unexpected error: %v", err)
	}
}

// TestCatalogValidateRejectsMissingRequiredFields table-drives corruption of required fields.
func TestCatalogValidateRejectsMissingRequiredFields(t *testing.T) {
	type tc struct {
		name   string
		mutate func(*catalog.Catalog)
	}
	cases := []tc{
		{
			name: "zero catalog_schema_version",
			mutate: func(c *catalog.Catalog) {
				c.CatalogSchemaVersion = 0
			},
		},
		{
			name: "empty generated_at",
			mutate: func(c *catalog.Catalog) {
				c.GeneratedAt = ""
			},
		},
		{
			name: "empty op_id",
			mutate: func(c *catalog.Catalog) {
				c.Ops[0].OpID = ""
			},
		},
		{
			name: "empty default_variant_id on op",
			mutate: func(c *catalog.Catalog) {
				c.Ops[0].DefaultVariantID = ""
			},
		},
		{
			name: "empty variant_id on variant",
			mutate: func(c *catalog.Catalog) {
				c.Ops[0].Variants[0].VariantID = ""
			},
		},
		{
			name: "empty binding adapter_key",
			mutate: func(c *catalog.Catalog) {
				c.Ops[0].Variants[0].Binding.AdapterKey = ""
			},
		},
		{
			name: "empty binding operation_key",
			mutate: func(c *catalog.Catalog) {
				c.Ops[0].Variants[0].Binding.OperationKey = ""
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := loadFixture(t, "sample-catalog.json")
			tc.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatalf("expected non-nil error for case %q, got nil", tc.name)
			}
		})
	}
}

// TestCatalogValidateRejectsUnknownRiskClass verifies that risk_class "purge" yields a typed error.
func TestCatalogValidateRejectsUnknownRiskClass(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.Ops[0].Variants[0].RiskClass = catalog.RiskClass("purge")
	err := c.Validate()
	if err == nil {
		t.Fatal("expected non-nil error for unknown risk_class, got nil")
	}
	if !errors.Is(err, catalog.ErrUnknownRiskClass) {
		t.Fatalf("expected ErrUnknownRiskClass, got %v", err)
	}
}

// TestCatalogValidateRejectsUnknownAuthStrategy verifies that auth_strategy "magic" yields a typed error.
func TestCatalogValidateRejectsUnknownAuthStrategy(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.Ops[0].Variants[0].AuthStrategy = catalog.AuthStrategy("magic")
	err := c.Validate()
	if err == nil {
		t.Fatal("expected non-nil error for unknown auth_strategy, got nil")
	}
	if !errors.Is(err, catalog.ErrUnknownAuthStrategy) {
		t.Fatalf("expected ErrUnknownAuthStrategy, got %v", err)
	}
}

// TestCatalogValidateRejectsDanglingDefaultVariantID verifies that default_variant_id pointing
// to a non-existent variant_id yields a typed error.
func TestCatalogValidateRejectsDanglingDefaultVariantID(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.Ops[0].DefaultVariantID = "gmail.v1.rest.users.messages.NONEXISTENT"
	err := c.Validate()
	if err == nil {
		t.Fatal("expected non-nil error for dangling default_variant_id, got nil")
	}
	if !errors.Is(err, catalog.ErrDanglingDefaultVariantID) {
		t.Fatalf("expected ErrDanglingDefaultVariantID, got %v", err)
	}
}

// TestCatalogValidateRejectsUnsupportedBindingSchemaVersion verifies that an unrecognized
// binding_schema_version yields a typed error.
func TestCatalogValidateRejectsUnsupportedBindingSchemaVersion(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.Ops[0].Variants[0].Binding.BindingSchemaVersion = 9999
	err := c.Validate()
	if err == nil {
		t.Fatal("expected non-nil error for unsupported binding_schema_version, got nil")
	}
	if !errors.Is(err, catalog.ErrUnsupportedBindingSchemaVersion) {
		t.Fatalf("expected ErrUnsupportedBindingSchemaVersion, got %v", err)
	}
}

// TestBackendBindingPerKindRejectsUnsupportedVersion locks the binding_schema_version
// contract across every BackendKind variant. Per docs/catalog-abi.md "Binding-version
// migration" and spec §5.4.1 step 4, an unsupported binding_schema_version must yield
// ErrUnsupportedBindingSchemaVersion regardless of which backend kind the variant uses.
//
// Coverage is per-backend because adapters are registered per BackendKind; each adapter
// MUST refuse to bind against an unrecognized schema version rather than silently degrade.
// Today only v=1 is supported (SupportedBindingSchemaVersions); the test pins the
// rejection contract so future v=2 bumps cannot accidentally widen acceptance.
func TestBackendBindingPerKindRejectsUnsupportedVersion(t *testing.T) {
	kinds := []catalog.BackendKind{
		catalog.BackendKindTypedRestSDK,
		catalog.BackendKindDiscoveryREST,
		catalog.BackendKindRawHTTP,
		catalog.BackendKindGRPCSDK,
		catalog.BackendKindMCPPlugin,
		catalog.BackendKindGRPCPlugin,
	}
	badVersions := []int{0, 2, 9999}

	for _, kind := range kinds {
		for _, bad := range badVersions {
			name := string(kind) + "/v=" + strconv.Itoa(bad)
			t.Run(name, func(t *testing.T) {
				c := loadFixture(t, "sample-catalog.json")
				c.Ops[0].Variants[0].BackendKind = kind
				c.Ops[0].Variants[0].Binding.BindingSchemaVersion = bad
				err := c.Validate()
				if err == nil {
					t.Fatalf("BackendKind=%s binding_schema_version=%d: expected ErrUnsupportedBindingSchemaVersion, got nil", kind, bad)
				}
				if !errors.Is(err, catalog.ErrUnsupportedBindingSchemaVersion) {
					t.Fatalf("BackendKind=%s binding_schema_version=%d: expected ErrUnsupportedBindingSchemaVersion, got %v", kind, bad, err)
				}
			})
		}
	}
}

// TestBackendBindingPerKindAcceptsV1 is the positive counterpart: every BackendKind
// variant accepts the currently-supported binding_schema_version=1 without error.
// This anchors the "old" axis of the §5.4.1 step 4 acceptance criterion ("covers both
// old and new binding schema version") for the v0.1 single-version era; when a v2 lands
// the test should be extended to cover both v=1 and v=2 per the migration procedure.
func TestBackendBindingPerKindAcceptsV1(t *testing.T) {
	kinds := []catalog.BackendKind{
		catalog.BackendKindTypedRestSDK,
		catalog.BackendKindDiscoveryREST,
		catalog.BackendKindRawHTTP,
		catalog.BackendKindGRPCSDK,
		catalog.BackendKindMCPPlugin,
		catalog.BackendKindGRPCPlugin,
	}
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			c := loadFixture(t, "sample-catalog.json")
			c.Ops[0].Variants[0].BackendKind = kind
			c.Ops[0].Variants[0].Binding.BindingSchemaVersion = 1
			if err := c.Validate(); err != nil {
				t.Fatalf("BackendKind=%s binding_schema_version=1: unexpected error: %v", kind, err)
			}
		})
	}
}

// TestBindingSchemaVersionInteger enforces the patch-version prohibition in
// docs/catalog-abi.md (binding-version migration rule 5): binding_schema_version
// is an integer, not a semver triple. JSON values with a decimal point or a
// string suffix MUST fail to decode rather than being silently coerced.
//
// Encodes the test-matrix.md row "TestBindingSchemaVersionInteger" (v0.1 CI gate).
func TestBindingSchemaVersionInteger(t *testing.T) {
	cases := []struct {
		name    string
		rawJSON string
	}{
		{"decimal", `{"binding_schema_version": 1.5, "adapter_key": "a", "operation_key": "b"}`},
		{"semver-triple string", `{"binding_schema_version": "1.0.1", "adapter_key": "a", "operation_key": "b"}`},
		{"plain string", `{"binding_schema_version": "1", "adapter_key": "a", "operation_key": "b"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b catalog.Binding
			err := json.Unmarshal([]byte(tc.rawJSON), &b)
			if err == nil {
				t.Fatalf("expected JSON decode error for %q, got nil (decoded as %+v)", tc.rawJSON, b)
			}
		})
	}
}

// TestCatalogJSONFieldNames marshals a zero-value Catalog and asserts the spec-mandated JSON key names appear.
func TestCatalogJSONFieldNames(t *testing.T) {
	c := catalog.Catalog{}
	b := mustMarshal(t, c)
	raw := string(b)

	requiredKeys := []string{
		`"catalog_schema_version"`,
		`"generated_at"`,
		`"generator_version"`,
		`"ops"`,
	}
	for _, k := range requiredKeys {
		if !strings.Contains(raw, k) {
			t.Errorf("marshaled Catalog missing expected JSON key %s; got: %s", k, raw)
		}
	}

	// Also check Variant field names via marshaling a Variant.
	v := catalog.Variant{
		VariantID:   "v",
		RiskClass:   catalog.RiskClassRead,
		BackendKind: catalog.BackendKindTypedRestSDK,
	}
	bv := mustMarshal(t, v)
	rawv := string(bv)
	variantKeys := []string{
		`"variant_id"`,
		`"variant_schema_version"`,
		`"risk_class"`,
		`"backend_kind"`,
		`"interface_kind"`,
		`"stability"`,
	}
	for _, k := range variantKeys {
		if !strings.Contains(rawv, k) {
			t.Errorf("marshaled Variant missing expected JSON key %s; got: %s", k, rawv)
		}
	}

	// Check Binding field names.
	bind := catalog.Binding{
		BindingSchemaVersion: 1,
		AdapterKey:           "a",
		OperationKey:         "b",
	}
	bb := mustMarshal(t, bind)
	rawb := string(bb)
	bindingKeys := []string{
		`"binding_schema_version"`,
		`"adapter_key"`,
		`"operation_key"`,
	}
	for _, k := range bindingKeys {
		if !strings.Contains(rawb, k) {
			t.Errorf("marshaled Binding missing expected JSON key %s; got: %s", k, rawb)
		}
	}
}

// TestCatalogValidateRejectsDuplicateOpID pins the audit fix: a second op with
// the same op_id is rejected (findOp returns the first, silently shadowing it).
func TestCatalogValidateRejectsDuplicateOpID(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.Ops = append(c.Ops, c.Ops[0]) // duplicate the first op
	if err := c.Validate(); !errors.Is(err, catalog.ErrDuplicateOpID) {
		t.Errorf("Validate() = %v; want ErrDuplicateOpID", err)
	}
}

// TestCatalogValidateRejectsDuplicateVariantID pins the audit fix: a duplicate
// variant_id within an op is rejected (resolveVariant shadows the second one).
func TestCatalogValidateRejectsDuplicateVariantID(t *testing.T) {
	c := loadFixture(t, "sample-catalog.json")
	c.Ops[0].Variants = append(c.Ops[0].Variants, c.Ops[0].Variants[0]) // duplicate a variant
	if err := c.Validate(); !errors.Is(err, catalog.ErrDuplicateVariantID) {
		t.Errorf("Validate() = %v; want ErrDuplicateVariantID", err)
	}
}
