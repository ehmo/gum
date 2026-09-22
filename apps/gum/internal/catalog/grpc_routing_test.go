package catalog_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestGrpcRoutingHeaderInvariant is the catalog-abi.md `routing_headers`
// acceptance for rules 1 to 4 (gum-dvn6). Each fixture is a one-op catalog
// that differs only in the routing_headers array, so the assertion is about
// the array and nothing else. Rule 5, stability under regeneration, is the
// gen-catalog override diffing pass and is not covered here.
func TestGrpcRoutingHeaderInvariant(t *testing.T) {
	cases := []struct {
		fixture string
		wantErr error
	}{
		{"present.json", nil},
		{"omitted.json", nil},
		{"not-required.json", catalog.ErrGRPCRoutingHeaderNotRequired},
		{"not-found.json", catalog.ErrGRPCRoutingHeaderNotFound},
		{"duplicate.json", catalog.ErrGRPCRoutingHeaderDuplicate},
		{"invalid-alphabet.json", catalog.ErrGRPCRoutingHeaderInvalid},
		{"array-indexing.json", catalog.ErrGRPCRoutingHeaderNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			cat := loadRoutingFixture(t, tc.fixture)
			err := cat.Validate()
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("Validate() = %v; want nil", err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("Validate() = %v; want %v", err, tc.wantErr)
			}
		})
	}
}

// TestRoutingHeaderFailsOnTheFirstBadEntry pins the reporting order: entries
// are checked in declaration order and the first failure is returned, so a
// list whose first entry does not resolve reports NOT_FOUND even though a
// later entry duplicates it.
func TestRoutingHeaderFailsOnTheFirstBadEntry(t *testing.T) {
	cat := loadRoutingFixture(t, "present.json")
	cat.Ops[0].Variants[0].Binding.RoutingHeaders = []string{"nowhere", "nowhere"}

	err := cat.Validate()
	if !errors.Is(err, catalog.ErrGRPCRoutingHeaderNotFound) {
		t.Fatalf("Validate() = %v; want the first entry to fail with NOT_FOUND", err)
	}
}

// TestRoutingHeaderRulesSkipNonGRPCVariants pins the scope: routing_headers is
// a grpc-sdk binding field, and the three build failures are defined for
// grpc-sdk alone.
func TestRoutingHeaderRulesSkipNonGRPCVariants(t *testing.T) {
	cat := loadRoutingFixture(t, "not-required.json")
	cat.Ops[0].Variants[0].InterfaceKind = catalog.InterfaceKindDiscoveryREST
	cat.Ops[0].Variants[0].BackendKind = catalog.BackendKindDiscoveryREST

	if err := cat.Validate(); err != nil {
		t.Fatalf("Validate() = %v; want nil for a non-grpc-sdk variant", err)
	}
}

// TestRoutingHeaderNeedsARequestSchema pins rule 2 against an op that declares
// no request fields: nothing resolves, so every entry fails.
func TestRoutingHeaderNeedsARequestSchema(t *testing.T) {
	cat := loadRoutingFixture(t, "present.json")
	cat.Ops[0].RequestFields = nil

	err := cat.Validate()
	if !errors.Is(err, catalog.ErrGRPCRoutingHeaderNotFound) {
		t.Fatalf("Validate() = %v; want %v", err, catalog.ErrGRPCRoutingHeaderNotFound)
	}
}

// TestRoutingHeaderRejectsAnUndeclarableRequestSchema covers the op whose
// request fields cannot be turned into a schema at all: rule 2 has nothing to
// resolve against, so validation reports the schema fault rather than a
// routing-header code.
func TestRoutingHeaderRejectsAnUndeclarableRequestSchema(t *testing.T) {
	cat := loadRoutingFixture(t, "present.json")
	cat.Ops[0].RequestFields[0].Name = ""

	err := cat.Validate()
	if err == nil || !strings.Contains(err.Error(), "request field with empty name") {
		t.Fatalf("Validate() = %v; want the empty-name schema fault", err)
	}
}

func loadRoutingFixture(t *testing.T, name string) *catalog.Catalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "grpc-routing", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var cat catalog.Catalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", name, err)
	}
	return &cat
}
