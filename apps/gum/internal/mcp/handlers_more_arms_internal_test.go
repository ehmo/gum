package mcp

import (
	"context"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/catalog"
)

// TestMakeConvenienceHandlerUnwiredToolReturnsConvenienceNotWired pins
// makeConvenienceHandler's `!ok → CONVENIENCE_NOT_WIRED` arm
// (handlers.go:67-69). When a convenience tool name has no entry in
// convenienceOpRouting (e.g., a leftover registration after the routing
// map was rolled back), the handler MUST return a stable
// CONVENIENCE_NOT_WIRED envelope so clients can detect the misconfig.
func TestMakeConvenienceHandlerUnwiredToolReturnsConvenienceNotWired(t *testing.T) {
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
	// "phantom.tool" is not in convenienceOpRouting — handler reaches the
	// !ok arm.
	handler := s.makeConvenienceHandler("phantom.tool")
	res, err := handler(context.Background(), buildReq(t, nil))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	body := errBodyOf(t, res)
	if !strings.Contains(body, "CONVENIENCE_NOT_WIRED") {
		t.Errorf("body=%q; want CONVENIENCE_NOT_WIRED", body)
	}
	if !strings.Contains(body, "phantom.tool") {
		t.Errorf("body=%q; want tool name in message", body)
	}
}

// TestHandleSearchAPIsCandidateKCapsAt50 pins handleSearchAPIs's
// `candidateK > 50 → cap at 50` arm (handlers.go:111-113). The k*5
// retrieval factor lets a small k still surface enough candidates to
// fill ProjectionTopK, but the cap protects against pathological
// large-k requests blowing past the BM25 hard ceiling.
//
// We invoke with k=20 (so k*5 = 100 > 50) against a tiny catalog;
// the cap is the only thing exercised — the call still returns a
// success envelope because the empty result set is valid output.
func TestHandleSearchAPIsCandidateKCapsAt50(t *testing.T) {
	// Catalog needs at least one op so the search-index path is taken.
	op := catalog.Op{
		OpID:             "test.op",
		OpSchemaVersion:  1,
		Title:            "Test",
		Summary:          "test op",
		DefaultVariantID: "v.1",
		Variants: []catalog.Variant{{
			VariantID:        "v.1",
			VariantSchemaVersion: 1,
			RiskClass:        catalog.RiskClassRead,
		}},
	}
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
		Ops:                  []catalog.Op{op},
	})
	req := buildReq(t, map[string]any{"query": "nonexistent", "k": 20})
	res, err := s.handleSearchAPIs(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSearchAPIs: %v", err)
	}
	// Should NOT be an error result — k=20 is valid, just the candidateK
	// branch caps internally.
	if res.IsError {
		t.Errorf("IsError=true; want false (k=20 is valid, cap is internal)")
	}
}

// TestShapeSearchAPIsRowExpectedResponseFromOutputProfile pins
// shapeSearchAPIsRow's `v.OutputProfile != "" → expectedResponse = ...`
// arm (handlers.go:154-156). When the default variant has an
// OutputProfile set, the shaped row's expected_response surfaces that
// profile name so clients see what shape they'll get.
func TestShapeSearchAPIsRowExpectedResponseFromOutputProfile(t *testing.T) {
	op := catalog.Op{
		OpID:             "test.op.profiled",
		OpSchemaVersion:  1,
		Title:            "Profiled",
		Summary:          "test",
		DefaultVariantID: "v.1",
		ParamsRequired:   [][]string{{"id"}},
		Variants: []catalog.Variant{{
			VariantID:        "v.1",
			VariantSchemaVersion: 1,
			RiskClass:        catalog.RiskClassRead,
			OutputProfile:    "test.profile.v1",
		}},
	}
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
		Ops:                  []catalog.Op{op},
	})
	req := buildReq(t, map[string]any{"query": "profiled", "k": 5})
	res, err := s.handleSearchAPIs(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSearchAPIs: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError=true; want false")
	}
	body := errBodyOf(t, res)
	// The shaped output should include the profile name as expected_response.
	if !strings.Contains(body, "test.profile.v1") {
		t.Errorf("body=%q; want expected_response='test.profile.v1' from OutputProfile arm", body)
	}
}

// TestHandlePollEmptyOperationNameReturnsInvalidArgs pins handlePoll's
// `operationName == "" → INVALID_ARGS` arm (handlers.go:316-318). A
// poll without an operation_name has nothing to resume and MUST fail
// fast with a structured INVALID_ARGS envelope.
func TestHandlePollEmptyOperationNameReturnsInvalidArgs(t *testing.T) {
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
	req := buildReq(t, map[string]any{})
	res, err := s.handlePoll(context.Background(), req)
	if err != nil {
		t.Fatalf("handlePoll: %v", err)
	}
	body := errBodyOf(t, res)
	if !strings.Contains(body, "INVALID_ARGS") {
		t.Errorf("body=%q; want INVALID_ARGS", body)
	}
	if !strings.Contains(body, "operation_name") {
		t.Errorf("body=%q; want 'operation_name' missing-key surface", body)
	}
}

// TestMakeMetaToolHandlerUnknownReturnsUnknownHandler pins the
// `default → s.handleUnknown(name)` arm in makeMetaToolHandler
// (handlers.go:57). Confirms the routing table fallback wires through
// to META_TOOL_NOT_IMPLEMENTED for any name not in the switch.
func TestMakeMetaToolHandlerUnknownReturnsUnknownHandler(t *testing.T) {
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
	handler := s.makeMetaToolHandler("gum.nonexistent_meta")
	res, err := handler(context.Background(), buildReq(t, nil))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	body := errBodyOf(t, res)
	if !strings.Contains(body, "META_TOOL_NOT_IMPLEMENTED") {
		t.Errorf("body=%q; want META_TOOL_NOT_IMPLEMENTED", body)
	}
}

// Avoid unused-import warnings when adding tests incrementally.
var _ = sdkmcp.CallToolResult{}
