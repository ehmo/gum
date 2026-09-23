// Package mcp — Red Team failing tests for gum-np38.10.
//
// Covers: gum.search_apis TOON output profile binding per spec §9.4.
//
// Spec anchors:
//   - spec.md §9.4: gum.search_apis → TOON profile (hardcoded, non-overridable)
//   - spec.md §4.1: tuple shape {api, op, summary, params_required, expected_response}
//   - expression-profile-dsl.md: collapse_arrays, truncate_strings, on_empty, recovery
//
// ALL 5 tests MUST FAIL until Green implements:
//   - searchAPIsProfile(k int) *profile.Profile in internal/mcp/
//   - handleSearchAPIs routing response through profile.Apply
package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/output/toon"
)

// ---------------------------------------------------------------------------
// Test 1: Profile shape
// ---------------------------------------------------------------------------

// TestSearchAPIsProfileShape asserts that searchAPIsProfile(k) returns a
// *profile.Profile matching spec §9.4 exactly.
func TestSearchAPIsProfileShape(t *testing.T) {
	prof := searchAPIsProfile(7, searchAPIsTuning{})
	if prof == nil {
		t.Fatal("searchAPIsProfile(7) returned nil")
	}

	// DefaultFormat must be "toon" — spec §9.4 implicit TOON profile.
	if prof.DefaultFormat != "toon" {
		t.Errorf("DefaultFormat=%q; want \"toon\" (spec §9.4)", prof.DefaultFormat)
	}

	// collapse_arrays.max_items must bind caller k.
	if prof.CollapseArrays == nil {
		t.Fatal("CollapseArrays is nil; want non-nil with MaxItems==7 (spec §9.4)")
	} else if prof.CollapseArrays.MaxItems != 7 {
		t.Errorf("CollapseArrays.MaxItems=%d; want 7 (spec §9.4: binds caller k)", prof.CollapseArrays.MaxItems)
	}

	// truncate_strings: default_chars=120.
	if prof.TruncateStrings == nil {
		t.Fatal("TruncateStrings is nil; want non-nil (spec §9.4)")
	} else {
		if prof.TruncateStrings.DefaultChars != 120 {
			t.Errorf("TruncateStrings.DefaultChars=%d; want 120 (spec §9.4)", prof.TruncateStrings.DefaultChars)
		}
		// truncate_strings.fields.summary = 80.
		if prof.TruncateStrings.Fields == nil {
			t.Error("TruncateStrings.Fields is nil; want {\"summary\":80} (spec §9.4)")
		} else if prof.TruncateStrings.Fields["summary"] != 80 {
			t.Errorf("TruncateStrings.Fields[\"summary\"]=%d; want 80 (spec §9.4)",
				prof.TruncateStrings.Fields["summary"])
		}
	}

	// on_empty sentinel.
	wantOnEmpty := "No matching operations found. Try a broader query."
	if prof.OnEmpty != wantOnEmpty {
		t.Errorf("OnEmpty=%q; want %q (spec §9.4)", prof.OnEmpty, wantOnEmpty)
	}

	// recovery = "none".
	if prof.Recovery != "none" {
		t.Errorf("Recovery=%q; want \"none\" (spec §9.4)", prof.Recovery)
	}

	// k binding is dynamic: k=3 must yield MaxItems==3.
	prof3 := searchAPIsProfile(3, searchAPIsTuning{})
	if prof3 == nil {
		t.Fatal("searchAPIsProfile(3) returned nil")
	}
	if prof3.CollapseArrays == nil {
		t.Fatal("searchAPIsProfile(3): CollapseArrays is nil")
	} else if prof3.CollapseArrays.MaxItems != 3 {
		t.Errorf("searchAPIsProfile(3): CollapseArrays.MaxItems=%d; want 3 (proves k binding is dynamic)",
			prof3.CollapseArrays.MaxItems)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Handler returns TOON, not JSON
// ---------------------------------------------------------------------------

// TestSearchAPIsHandlerReturnsTOON verifies that handleSearchAPIs runs the
// result through profile.Apply and returns TOON-formatted TextContent.
func TestSearchAPIsHandlerReturnsTOON(t *testing.T) {
	opID := "example.read.thing"
	variantID := opID + ".v1"
	op := catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            "Example Read Thing",
		Summary:          "Reads an example thing by id",
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

	raw, _ := json.Marshal(map[string]any{
		"query": "read thing",
		"k":     5,
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
				t.Fatalf("handleSearchAPIs returned error result: %s", tc.Text)
			}
		}
		t.Fatal("handleSearchAPIs returned IsError=true")
	}

	if len(res.Content) < 1 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T; want *sdkmcp.TextContent", res.Content[0])
	}
	text := tc.Text

	// Spec §9.4 TOON column header must be present (homogeneous array encoding).
	// TOON encoder emits sorted keys as the header row; spec §9.4 fields are
	// api, op, summary, params_required, expected_response.
	// All five field names must appear in the header line.
	for _, col := range []string{"api", "op", "summary", "params_required", "expected_response"} {
		if !strings.Contains(text, col) {
			t.Errorf("TOON body missing column %q; body: %s", col, text)
		}
	}

	// The column header must appear as a CSV row — all 5 fields comma-separated on one line.
	// Spec §9.4 exact field order: api,expected_response,op,params_required,summary
	// (TOON encoder sorts keys alphabetically for homogeneous arrays).
	wantHeader := "api,expected_response,op,params_required,summary"
	if !strings.Contains(text, wantHeader) {
		t.Errorf("TOON body missing exact header row %q;\nbody:\n%s", wantHeader, text)
	}

	// Body must contain the op ID and summary.
	if !strings.Contains(text, "example.read.thing") {
		t.Errorf("TOON body missing op id %q; body: %s", "example.read.thing", text)
	}
	if !strings.Contains(text, "Reads an example thing") {
		t.Errorf("TOON body missing summary fragment %q; body: %s", "Reads an example thing", text)
	}

	// Negative: body must NOT start with '{' — it is TOON, not JSON.
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		t.Errorf("TOON body starts with '{' — handler still returns JSON (spec §9.4 requires TOON); body: %s", text)
	}
}

// ---------------------------------------------------------------------------
// Test 3: on_empty fires for zero matches
// ---------------------------------------------------------------------------

// TestSearchAPIsOnEmptyFires verifies that the on_empty sentinel is emitted
// when no ops match the query (spec §9.4 on_empty).
func TestSearchAPIsOnEmptyFires(t *testing.T) {
	opID := "example.read.thing"
	variantID := opID + ".v1"
	op := catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            "Example Read Thing",
		Summary:          "Reads an example thing by id",
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

	raw, _ := json.Marshal(map[string]any{
		"query": "zzzz_no_match",
		"k":     5,
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
		t.Fatal("handleSearchAPIs returned IsError=true on empty match; want IsError=false")
	}

	if len(res.Content) < 1 {
		t.Fatal("result has no content")
	}

	wantSentinel := "No matching operations found. Try a broader query."

	// The sentinel reaches the caller in a text block. It is its own block, not
	// a substitute for the payload: replacing the body with the string left a
	// caller unable to tell an empty result from a string-valued one (§9.1).
	var texts []string
	for _, c := range res.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	if !slices.ContainsFunc(texts, func(t string) bool { return strings.Contains(t, wantSentinel) }) {
		t.Errorf("on_empty sentinel missing from every text block;\nwant substring: %q\ngot: %q", wantSentinel, texts)
	}

	// §9.1 rule 2: the machine-readable copy is _expression.on_empty_message.
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structuredContent is %T; want the §13 ToonResult object", res.StructuredContent)
	}
	meta, ok := sc["_expression"].(*dispatch.ExpressionMeta)
	if !ok {
		t.Fatalf("_expression is %T; want *dispatch.ExpressionMeta", sc["_expression"])
	}
	if meta.OnEmptyMessage == nil {
		t.Fatal("_expression.on_empty_message is null; want the profile's on_empty string")
	}
	if *meta.OnEmptyMessage != wantSentinel {
		t.Errorf("_expression.on_empty_message = %q; want %q", *meta.OnEmptyMessage, wantSentinel)
	}
	if meta.ResultCount != 0 {
		t.Errorf("_expression.result_count = %d; want 0", meta.ResultCount)
	}
}

// ---------------------------------------------------------------------------
// Test 4: truncate_strings applies to summary (80-char per-field limit)
// ---------------------------------------------------------------------------

// TestSearchAPIsTruncateStringsApplies verifies that the per-field truncation
// limit of 80 characters is applied to "summary" (spec §9.4).
func TestSearchAPIsTruncateStringsApplies(t *testing.T) {
	// Build a 200-char summary — well beyond the 80-char per-field limit.
	longSummary := strings.Repeat("a", 200)

	opID := "example.long.summary"
	variantID := opID + ".v1"
	op := catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            "Long Summary Op",
		Summary:          longSummary,
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

	raw, _ := json.Marshal(map[string]any{
		"query": "long summary",
		"k":     5,
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
		t.Fatal("handleSearchAPIs returned IsError=true")
	}

	if len(res.Content) < 1 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T; want *sdkmcp.TextContent", res.Content[0])
	}
	text := tc.Text

	// The 200-char run of 'a' must NOT appear intact in the body.
	if strings.Contains(text, longSummary) {
		t.Errorf("full 200-char summary appears in body — truncate_strings not applied (spec §9.4 summary limit=80)")
	}

	// At most 80 consecutive 'a' chars should appear (the truncated cell).
	// Find the longest run of 'a' in text.
	maxRun := 0
	current := 0
	for _, ch := range text {
		if ch == 'a' {
			current++
			if current > maxRun {
				maxRun = current
			}
		} else {
			current = 0
		}
	}
	if maxRun > 80 {
		t.Errorf("longest run of 'a' in body=%d; want <=80 after truncate_strings{summary:80} (spec §9.4)", maxRun)
	}

	// The 81st 'a' must not be present after the truncated cell.
	// Construct the 81-char prefix and assert it's absent.
	run81 := strings.Repeat("a", 81)
	if strings.Contains(text, run81) {
		t.Errorf("body contains 81+ consecutive 'a' chars — truncation to 80 not applied (spec §9.4)")
	}
}

// ---------------------------------------------------------------------------
// Test 5: collapse_arrays applies — k limits result count
// ---------------------------------------------------------------------------

// TestSearchAPIsCollapseAtK verifies that when more than k results exist,
// the TOON body shows exactly k data rows and an omitted_count marker
// (spec §9.4: collapse_arrays.max_items=k).
func TestSearchAPIsCollapseAtK(t *testing.T) {
	// Build 6 ops all matching "match-token".
	ops := make([]catalog.Op, 6)
	for i := range ops {
		id := strings.Repeat("match", 1) + ".token.op" + string(rune('0'+i))
		variantID := id + ".v1"
		ops[i] = catalog.Op{
			OpID:             id,
			OpSchemaVersion:  1,
			Title:            "Match Token Op",
			Summary:          "match-token operation " + string(rune('0'+i)),
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
	}
	snap := minimalCatalog(ops...)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	raw, _ := json.Marshal(map[string]any{
		"query": "match-token",
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
		t.Fatal("handleSearchAPIs returned IsError=true")
	}

	if len(res.Content) < 1 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T; want *sdkmcp.TextContent", res.Content[0])
	}
	text := tc.Text

	// When >k results exist and CollapseArrays fires, applyCollapseArrays wraps
	// the top-level array as {"items":[<k items>],"omitted_count":<N>}. In a
	// §9.0 document the array becomes the rows and omitted_count rides the
	// header, so the count is read off the decoded document.
	doc, err := toon.DecodeTOONDocument([]byte(text))
	if err != nil {
		t.Fatalf("DecodeTOONDocument: %v;\nbody:\n%s", err, text)
	}
	if doc.RecordKey != "items" {
		t.Errorf("records header = %q; want \"items\" (collapse_arrays wraps under items);\nbody:\n%s", doc.RecordKey, text)
	}
	if doc.Count > 3 {
		t.Errorf("count = %d; want at most 3 (k=3 limit, spec §9.4);\nbody:\n%s", doc.Count, text)
	}

	// The k=3 binding held only if collapse dropped rows, so omitted_count must
	// be present and positive.
	omitted, found := headerNumber(doc, "omitted_count")
	if !found {
		t.Fatalf("TOON header missing \"omitted_count\" — collapse_arrays not applied (spec §9.4 k=3 against 6 results);\nbody:\n%s", text)
	}
	if omitted <= 0 {
		t.Errorf("omitted_count = %v; want > 0 with k=3 and 6 matching ops;\nbody:\n%s", omitted, text)
	}
}

// headerNumber reads one numeric extra header off a decoded §9.0 document.
func headerNumber(doc *toon.TOONDocument, key string) (float64, bool) {
	for _, h := range doc.Extra {
		if h.Key != key {
			continue
		}
		f, ok := h.Value.(float64)
		return f, ok
	}
	return 0, false
}

// Compile-time check: ensure profile package is used (prevents import-not-used error
// if the test file compiles after Green ships but before assertions are adjusted).
var _ *profile.Profile
