// Package mcp — Red Team failing tests for gum-9vuq.1.
//
// Covers: gum.search_apis input schema (param name k, bounds, required),
// MCP annotations (readOnlyHint=true, destructiveHint=false), and handler
// result shape ({api, op, summary, params_required, expected_response} per row).
//
// Spec anchors:
//   - spec.md §4.1 line 291: gum.search_apis(query, k=5)
//   - spec.md §2139 meta_tools.search_apis.k: default=5, range 1–20
//   - spec.md §13 line 3220 annotations table: readOnlyHint=true, destructiveHint=false
//
// All 5 tests MUST FAIL until production code is fixed.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/catalog"
)

// --- Test 1: Schema property name is "k", not "top_k" -------------------------

// TestGumSearchAPIsSchemaParamNameK asserts that the gum.search_apis input schema
// contains property "k" and does NOT contain property "top_k".
//
// Spec anchor: spec.md §4.1 line 291 — gum.search_apis(query, k=5).
// Current schema (schemas.go line 129) declares "top_k" — wrong name.
func TestGumSearchAPIsSchemaParamNameK(t *testing.T) {
	raw := metaToolSchema("gum.search_apis")
	if len(raw) == 0 {
		t.Fatal("metaToolSchema(gum.search_apis) returned empty schema")
	}

	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties object")
	}

	// Must have "k"
	if _, exists := props["k"]; !exists {
		t.Error(`schema missing property "k" (spec §4.1 line 291: gum.search_apis(query, k=5))`)
	}

	// Must NOT have "top_k"
	if _, exists := props["top_k"]; exists {
		t.Error(`schema must not contain property "top_k"; spec §4.1 line 291 uses "k"`)
	}
}

// --- Test 2: k integer, default=5, minimum=1, maximum=20 ---------------------

// TestGumSearchAPIsSchemaKBounds asserts that the "k" property is type integer
// with default=5, minimum=1, and maximum=20.
//
// Spec anchor: spec.md §2139 meta_tools.search_apis.k — default 5, range 1–20.
// Current schema has "top_k" with maximum=50 — both name and bound are wrong.
func TestGumSearchAPIsSchemaKBounds(t *testing.T) {
	raw := metaToolSchema("gum.search_apis")
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties object")
	}

	kProp, exists := props["k"]
	if !exists {
		t.Fatal(`property "k" not found in schema; cannot check bounds`)
	}

	kMap, ok := kProp.(map[string]any)
	if !ok {
		t.Fatalf("property k is not an object; got %T", kProp)
	}

	// type must be "integer"
	if typ, _ := kMap["type"].(string); typ != "integer" {
		t.Errorf(`k.type=%q; want "integer"`, typ)
	}

	// default must be 5
	if def, ok := kMap["default"]; !ok {
		t.Error(`k missing "default" field (spec §2139: default=5)`)
	} else if def != float64(5) {
		t.Errorf("k.default=%v; want 5 (spec §2139)", def)
	}

	// minimum must be 1
	if min, ok := kMap["minimum"]; !ok {
		t.Error(`k missing "minimum" field (spec §2139: range 1–20)`)
	} else if min != float64(1) {
		t.Errorf("k.minimum=%v; want 1 (spec §2139)", min)
	}

	// maximum must be 20
	if max, ok := kMap["maximum"]; !ok {
		t.Error(`k missing "maximum" field (spec §2139: range 1–20)`)
	} else if max != float64(20) {
		t.Errorf("k.maximum=%v; want 20 (spec §2139, current has 50 — wrong)", max)
	}
}

// --- Test 3: required=["query"], additionalProperties:false ------------------

// TestGumSearchAPIsSchemaRequiredOnlyQuery asserts that the required array
// contains exactly ["query"] and that additionalProperties is false.
//
// Spec anchor: spec.md §4.1 — query required; k optional with default.
// §4.1 criterion 4 — additionalProperties:false on all tool schemas.
func TestGumSearchAPIsSchemaRequiredOnlyQuery(t *testing.T) {
	raw := metaToolSchema("gum.search_apis")
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	// additionalProperties must be false
	addl, ok := s["additionalProperties"].(bool)
	if !ok || addl {
		t.Error("schema must have additionalProperties:false (spec §4.1 criterion 4)")
	}

	// required must be exactly ["query"]
	required, _ := s["required"].([]any)
	if len(required) != 1 {
		t.Errorf("required must have exactly 1 entry [\"query\"]; got %v", required)
	} else if required[0] != "query" {
		t.Errorf("required[0]=%q; want \"query\"", required[0])
	}
}

// --- Test 4: TierAMetaToolAnnotations has gum.search_apis readOnlyHint=true --

// TestGumSearchAPIsAnnotationReadOnlyHintTrue asserts that TierAMetaToolAnnotations()
// returns an entry for "gum.search_apis" with ReadOnlyHint=true and
// DestructiveHint=*bool(false).
//
// Spec anchor: spec.md §13 line 3220 annotations table —
// gum.search_apis: readOnlyHint=true, destructiveHint=false.
// Current TierAMetaToolAnnotations() has no entry for "gum.search_apis".
func TestGumSearchAPIsAnnotationReadOnlyHintTrue(t *testing.T) {
	ann := TierAMetaToolAnnotations()

	entry, exists := ann["gum.search_apis"]
	if !exists {
		t.Fatal(`TierAMetaToolAnnotations() missing entry for "gum.search_apis" (spec §13 line 3220)`)
	}
	if entry == nil {
		t.Fatal(`TierAMetaToolAnnotations()["gum.search_apis"] is nil`)
	}

	// readOnlyHint must be true
	if !entry.ReadOnlyHint {
		t.Errorf("gum.search_apis Annotations.ReadOnlyHint=%v; want true (spec §13)", entry.ReadOnlyHint)
	}

	// destructiveHint must be explicitly *false
	if entry.DestructiveHint == nil {
		t.Error("gum.search_apis Annotations.DestructiveHint is nil; want explicit *false (spec §13)")
	} else if *entry.DestructiveHint {
		t.Errorf("gum.search_apis Annotations.DestructiveHint=%v; want false (spec §13)", *entry.DestructiveHint)
	}
}

// --- Test 5: Handler result row shape has 5 required keys --------------------

// TestGumSearchAPIsResultShape invokes handleSearchAPIs with a minimal catalog
// containing one op and asserts that:
//   - the envelope is {"results":[...]}
//   - each result row has keys: api, op, summary, params_required, expected_response
//
// Spec anchor: spec.md §4.1 line 291 — result tuples contain
// {api, op, summary, params_required, expected_response}.
//
// Current handler returns raw embed.SearchResult rows with keys op_id/score/
// summary/risk_class/auth_strategy — wrong shape. This test MUST FAIL until
// the handler maps results to the spec-required tuple shape.
func TestGumSearchAPIsResultShape(t *testing.T) {
	// Build a minimal catalog with one op that has a required param.
	// ParamsRequired is on Op (not Variant) per catalog/types.go.
	opID := "example.read.thing"
	variantID := opID + ".v1"
	op := catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            "Example Read Thing",
		Summary:          "Reads an example thing by id",
		ParamsRequired:   [][]string{{"thing_id"}},
		DefaultVariantID: variantID,
		Variants: []catalog.Variant{
			{
				VariantID:     variantID,
				Stability:     catalog.StabilityStable,
				InterfaceKind: catalog.InterfaceKindDiscoveryREST,
				BackendKind:   catalog.BackendKindDiscoveryREST,
				RiskClass:     catalog.RiskClassRead,
			},
		},
	}
	snap := minimalCatalog(op)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	// Build a request using param name "k" (spec §4.1) — not "top_k".
	raw, _ := json.Marshal(map[string]any{
		"query": "read thing",
		"k":     3,
	})
	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      "gum.search_apis",
			Arguments: raw,
		},
	}

	res, err := srv.handleSearchAPIs(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSearchAPIs returned unexpected Go error: %v", err)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if res.IsError {
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
				t.Fatalf("unexpected error result: %s", tc.Text)
			}
		}
		t.Fatal("handleSearchAPIs returned error result")
	}

	// Parse the TOON body. Spec §4.1 line 291: tuple shape
	// {api, op, summary, params_required, expected_response}.
	// Handler now routes through profile.Apply (spec §2129) — output is TOON, not JSON.
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is not TextContent; got %T", res.Content[0])
	}
	text := tc.Text

	// TOON body must NOT start with '{' — it is not JSON.
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		t.Errorf("result starts with '{' — handler still returns JSON; want TOON (spec §2129); body: %s", text)
	}

	// TOON encoder emits sorted keys as the header row for homogeneous arrays.
	// Spec §2129 field order (alphabetical): api, expected_response, op, params_required, summary.
	wantHeader := "api,expected_response,op,params_required,summary"
	if !strings.Contains(text, wantHeader) {
		t.Errorf("TOON body missing header row %q (spec §4.1 line 291 tuple shape); body:\n%s", wantHeader, text)
	}

	// Body must contain the op id and summary fragment.
	if !strings.Contains(text, "example.read.thing") {
		t.Errorf("TOON body missing op id %q; body: %s", "example.read.thing", text)
	}
	if !strings.Contains(text, "Reads an example thing") {
		// BM25 should match "read thing" against "Reads an example thing by id".
		// If the fragment is absent the handler may be returning nothing.
		t.Errorf("TOON body missing summary fragment %q (spec §4.1 line 291); body: %s",
			"Reads an example thing", text)
	}
}

