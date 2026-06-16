// gum-45d acceptance: Docs, Sheets, Slides Tier A convenience tools.
// Spec §4.1 lines 359-363 — five ops: docs.documents.get, docs.documents.create,
// sheets.spreadsheets.values.get, sheets.spreadsheets.values.update,
// slides.presentations.get. Each backs a convenience tool already registered
// in internal/mcp/tier_a_abi.go; these tests pin the catalog entries that
// dispatch.resolveVariant needs so the convenience handlers can reach a
// real adapter.

package main

import (
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// expectedWorkspaceOp captures the per-op invariants for the five Tier A
// Workspace ops that gum-45d adds. Keeping them in a table keeps the test
// noise low and the spec-mapping obvious.
type expectedWorkspaceOp struct {
	OpID       string
	VariantID  string
	Service    string
	RiskClass  catalog.RiskClass
	Scope      string
	Method     string
	PathPrefix string // hostname prefix; full path varies by op
	GoPkg      string
	GoCall     string
}

// allWorkspaceTierAOps returns the union of Docs+Sheets+Slides ops in the
// order produced by main.go so the test exercises the exact wiring shipped.
func allWorkspaceTierAOps() []catalog.Op {
	var out []catalog.Op
	out = append(out, BuildDocsOps()...)
	out = append(out, BuildSheetsOps()...)
	out = append(out, BuildSlidesOps()...)
	return out
}

// TestDocsSheetsSlidesOpsShape pins op_id, variant_id, risk_class, scope,
// HTTP method, host prefix, go_pkg, and go_call for every Workspace Tier A
// op added by gum-45d. One row per spec §4.1 entry.
func TestDocsSheetsSlidesOpsShape(t *testing.T) {
	want := []expectedWorkspaceOp{
		{
			OpID:       "docs.documents.get",
			VariantID:  "docs.v1.rest.documents.get",
			Service:    "docs",
			RiskClass:  catalog.RiskClassRead,
			Scope:      "https://www.googleapis.com/auth/documents.readonly",
			Method:     "GET",
			PathPrefix: "https://docs.googleapis.com/v1/documents/",
			GoPkg:      "google.golang.org/api/docs/v1",
			GoCall:     "Documents.Get",
		},
		{
			OpID:       "docs.documents.create",
			VariantID:  "docs.v1.rest.documents.create",
			Service:    "docs",
			RiskClass:  catalog.RiskClassWrite,
			Scope:      "https://www.googleapis.com/auth/documents",
			Method:     "POST",
			PathPrefix: "https://docs.googleapis.com/v1/documents",
			GoPkg:      "google.golang.org/api/docs/v1",
			GoCall:     "Documents.Create",
		},
		{
			OpID:       "sheets.spreadsheets.values.get",
			VariantID:  "sheets.v4.rest.spreadsheets.values.get",
			Service:    "sheets",
			RiskClass:  catalog.RiskClassRead,
			Scope:      "https://www.googleapis.com/auth/spreadsheets.readonly",
			Method:     "GET",
			PathPrefix: "https://sheets.googleapis.com/v4/spreadsheets/",
			GoPkg:      "google.golang.org/api/sheets/v4",
			GoCall:     "Spreadsheets.Values.Get",
		},
		{
			OpID:       "sheets.spreadsheets.values.update",
			VariantID:  "sheets.v4.rest.spreadsheets.values.update",
			Service:    "sheets",
			RiskClass:  catalog.RiskClassWrite,
			Scope:      "https://www.googleapis.com/auth/spreadsheets",
			Method:     "PUT",
			PathPrefix: "https://sheets.googleapis.com/v4/spreadsheets/",
			GoPkg:      "google.golang.org/api/sheets/v4",
			GoCall:     "Spreadsheets.Values.Update",
		},
		{
			OpID:       "slides.presentations.get",
			VariantID:  "slides.v1.rest.presentations.get",
			Service:    "slides",
			RiskClass:  catalog.RiskClassRead,
			Scope:      "https://www.googleapis.com/auth/presentations.readonly",
			Method:     "GET",
			PathPrefix: "https://slides.googleapis.com/v1/presentations/",
			GoPkg:      "google.golang.org/api/slides/v1",
			GoCall:     "Presentations.Get",
		},
	}

	got := allWorkspaceTierAOps()
	// Full Docs+Sheets+Slides surface — update when these services grow.
	const wantTotal = 15 // docs 3 + sheets 9 + slides 3
	if len(got) != wantTotal {
		t.Fatalf("got %d Workspace ops; want %d", len(got), wantTotal)
	}
	byID := map[string]catalog.Op{}
	for _, op := range got {
		byID[op.OpID] = op
	}
	// Every op is well-formed (shared discovery-rest/typed-rest-sdk shape).
	for _, op := range got {
		if op.ServiceFamily != "workspace" {
			t.Errorf("%s: service_family=%q want workspace", op.OpID, op.ServiceFamily)
		}
		if len(op.Variants) != 1 {
			t.Errorf("%s: variants=%d want 1", op.OpID, len(op.Variants))
			continue
		}
		v := op.Variants[0]
		if v.InterfaceKind != catalog.InterfaceKindDiscoveryREST || v.BackendKind != catalog.BackendKindTypedRestSDK ||
			v.AuthStrategy != catalog.AuthStrategyBYOOAuth || v.Binding == nil || v.Binding.HTTP == nil {
			t.Errorf("%s: variant/binding shape", op.OpID)
		}
		switch v.RiskClass {
		case catalog.RiskClassRead, catalog.RiskClassWrite, catalog.RiskClassDestructive:
		default:
			t.Errorf("%s: unexpected risk_class %q", op.OpID, v.RiskClass)
		}
	}
	// Detailed contract checks for the spec-pinned ops.
	for _, w := range want {
		op, ok := byID[w.OpID]
		if !ok {
			t.Errorf("op %q missing from Workspace Tier A set", w.OpID)
			continue
		}
		if op.Service != w.Service {
			t.Errorf("%s: service=%q want %q", w.OpID, op.Service, w.Service)
		}
		if op.ServiceFamily != "workspace" {
			t.Errorf("%s: service_family=%q want %q", w.OpID, op.ServiceFamily, "workspace")
		}
		if op.DefaultVariantID != w.VariantID {
			t.Errorf("%s: default_variant_id=%q want %q", w.OpID, op.DefaultVariantID, w.VariantID)
		}
		if len(op.Variants) != 1 {
			t.Errorf("%s: variants=%d want 1", w.OpID, len(op.Variants))
			continue
		}
		v := op.Variants[0]
		if v.VariantID != w.VariantID {
			t.Errorf("%s: variant_id=%q want %q", w.OpID, v.VariantID, w.VariantID)
		}
		if v.RiskClass != w.RiskClass {
			t.Errorf("%s: risk_class=%q want %q", w.OpID, v.RiskClass, w.RiskClass)
		}
		if v.InterfaceKind != catalog.InterfaceKindDiscoveryREST {
			t.Errorf("%s: interface_kind=%q want discovery-rest", w.OpID, v.InterfaceKind)
		}
		if v.BackendKind != catalog.BackendKindTypedRestSDK {
			t.Errorf("%s: backend_kind=%q want typed-rest-sdk", w.OpID, v.BackendKind)
		}
		if v.AuthStrategy != catalog.AuthStrategyBYOOAuth {
			t.Errorf("%s: auth_strategy=%q want byo_oauth", w.OpID, v.AuthStrategy)
		}
		if !v.Preferred {
			t.Errorf("%s: preferred=false want true (single-variant default)", w.OpID)
		}
		if len(v.Scopes) != 1 || v.Scopes[0] != w.Scope {
			t.Errorf("%s: scopes=%v want [%q]", w.OpID, v.Scopes, w.Scope)
		}
		if v.Binding == nil {
			t.Errorf("%s: binding is nil", w.OpID)
			continue
		}
		if v.Binding.AdapterKey != "rest.typed-rest-sdk" {
			t.Errorf("%s: adapter_key=%q want rest.typed-rest-sdk", w.OpID, v.Binding.AdapterKey)
		}
		if v.Binding.OperationKey != w.OpID {
			t.Errorf("%s: operation_key=%q want %q", w.OpID, v.Binding.OperationKey, w.OpID)
		}
		if v.Binding.GoPkg != w.GoPkg {
			t.Errorf("%s: go_pkg=%q want %q", w.OpID, v.Binding.GoPkg, w.GoPkg)
		}
		if v.Binding.GoCall != w.GoCall {
			t.Errorf("%s: go_call=%q want %q", w.OpID, v.Binding.GoCall, w.GoCall)
		}
		if v.Binding.HTTP == nil {
			t.Errorf("%s: binding.http is nil", w.OpID)
			continue
		}
		if v.Binding.HTTP.Method != w.Method {
			t.Errorf("%s: http.method=%q want %q", w.OpID, v.Binding.HTTP.Method, w.Method)
		}
		if got := v.Binding.HTTP.Path; len(got) < len(w.PathPrefix) || got[:len(w.PathPrefix)] != w.PathPrefix {
			t.Errorf("%s: http.path=%q does not start with %q", w.OpID, got, w.PathPrefix)
		}
	}
}

// TestDocsSheetsSlidesOpsValidate ensures the combined catalog passes
// catalog.Validate so the embedded snapshot won't reject these ops at load.
func TestDocsSheetsSlidesOpsValidate(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  allWorkspaceTierAOps(),
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("validate Workspace Tier A catalog: %v", err)
	}
}

// TestDocsSheetsSlidesOpsRejectsGUMOAuth pins that the v0.1.0-disabled
// gum_oauth strategy is never produced for these variants (bd memory:
// gum-auth-strategy-v3).
func TestDocsSheetsSlidesOpsRejectsGUMOAuth(t *testing.T) {
	for _, op := range allWorkspaceTierAOps() {
		for _, v := range op.Variants {
			if v.AuthStrategy == catalog.AuthStrategyGUMOAuth {
				t.Errorf("op %s variant %s: gum_oauth is disabled in v0.1.0", op.OpID, v.VariantID)
			}
		}
	}
}

// TestDocsSheetsSlidesOpsCoverConvenienceABI proves every convenience tool in
// internal/mcp/tier_a_abi.go that targets a docs.*, sheets.*, or slides.* op
// now has a matching catalog entry. Reverse linkage of the wiring contract.
func TestDocsSheetsSlidesOpsCoverConvenienceABI(t *testing.T) {
	requiredOpIDs := map[string]bool{
		"docs.documents.get":                false,
		"docs.documents.create":             false,
		"sheets.spreadsheets.values.get":    false,
		"sheets.spreadsheets.values.update": false,
		"slides.presentations.get":          false,
	}
	for _, op := range allWorkspaceTierAOps() {
		if _, want := requiredOpIDs[op.OpID]; want {
			requiredOpIDs[op.OpID] = true
		}
	}
	for id, present := range requiredOpIDs {
		if !present {
			t.Errorf("convenience tool target %q has no catalog op; convenience handler will surface OP_NOT_FOUND", id)
		}
	}
}
