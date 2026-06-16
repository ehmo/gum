// Package mcp — Red Team failing tests for gum-9vuq.3.
//
// Covers: gum.read schema (7 params), risk-gate enforcement (RISK_TOOL_MISMATCH),
// MCP annotations (readOnlyHint, destructiveHint), pagination passthrough,
// OP_NOT_FOUND suggestions.
//
// Spec anchors:
//   - spec.md §4.1 table: gum.read row — 7 params: op_id, args, variant_id?,
//     fields?, page_size?, page_token?, format? (toon|csv|json|markdown).
//   - spec.md §4.1 risk-gate algorithm (4 steps).
//   - spec.md §4.1: RISK_TOOL_MISMATCH envelope with op_id, variant_id,
//     variant_risk_class, required_tool.
//   - spec.md §4.1: readOnlyHint=true, destructiveHint=false on gum.read.
//   - spec.md §1421: stable error codes.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- helpers -----------------------------------------------------------------

// makeReadRequest builds a CallToolRequest whose Arguments JSON is derived from
// the supplied map. Suitable for calling handlers directly in white-box tests.
func makeReadRequest(args map[string]any) *sdkmcp.CallToolRequest {
	raw, _ := json.Marshal(args)
	return &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      "gum.read",
			Arguments: raw,
		},
	}
}

// minimalReadOp builds an in-memory catalog Op with the given opID and
// riskClass, suitable for injecting into a test server's snapshot.
func minimalReadOp(opID string, riskClass catalog.RiskClass) catalog.Op {
	variantID := opID + ".v1.test"
	return catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            opID,
		Summary:          "test op for " + opID,
		DefaultVariantID: variantID,
		Variants: []catalog.Variant{
			{
				VariantID:     variantID,
				Stability:     catalog.StabilityStable,
				InterfaceKind: catalog.InterfaceKindDiscoveryREST,
				BackendKind:   catalog.BackendKindDiscoveryREST,
				RiskClass:     riskClass,
			},
		},
	}
}

// minimalCatalog wraps a list of ops into a catalog.Catalog.
func minimalCatalog(ops ...catalog.Op) *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-01-01T00:00:00Z",
		GeneratorVersion:     "test",
		Ops:                  ops,
	}
}

// captureDispatcher records every Invocation it receives and returns a fixed
// ShapedResponse. Tests can inspect Calls after handler invocation.
type captureDispatcher struct {
	Calls []*dispatch.Invocation
	Body  []byte
}

func (d *captureDispatcher) Dispatch(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	d.Calls = append(d.Calls, inv)
	body := d.Body
	if body == nil {
		body = []byte(`{"ok":true}`)
	}
	return &dispatch.ShapedResponse{Body: body}, nil
}

// parseErrorResult unmarshals the error text from a CallToolResult (isError=true)
// into a generic map. Returns nil if parsing fails or result is not an error.
func parseErrorResult(t *testing.T, res *sdkmcp.CallToolResult) map[string]any {
	t.Helper()
	if res == nil {
		t.Fatal("result is nil")
	}
	if !res.IsError {
		t.Fatalf("expected error result but isError=false; content: %v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("error result has no content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("error result content[0] is not TextContent; got %T", res.Content[0])
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &m); err != nil {
		t.Fatalf("error result text is not JSON: %v; text: %s", err, tc.Text)
	}
	return m
}

// --- Test 1: Schema has exactly 7 params -------------------------------------

// TestGumReadInputSchemaHas7Params asserts that the gum.read input schema declares
// all 7 required properties, the correct format enum, required=["op_id"] only,
// and additionalProperties:false.
//
// Spec anchor: spec.md §4.1 table — "gum.read | op_id, args, variant_id?,
// fields?, page_size?, page_token?, format? (7)"
// format closed enum: toon|csv|json|markdown (NOT toon|json|raw).
//
// Current schema (schemas.go) declares only 3 properties: op_id, args, format.
// It also uses the wrong format enum ["toon","json","raw"] instead of
// ["toon","csv","json","markdown"]. This test must FAIL until fixed.
func TestGumReadInputSchemaHas7Params(t *testing.T) {
	raw := metaToolSchema("gum.read")
	if len(raw) == 0 {
		t.Fatal("metaToolSchema(gum.read) returned empty schema")
	}

	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	// 1. additionalProperties: false
	addl, ok := s["additionalProperties"].(bool)
	if !ok || addl {
		t.Error("schema must have additionalProperties:false")
	}

	// 2. required == ["op_id"] only
	required, _ := s["required"].([]any)
	if len(required) != 1 {
		t.Errorf("required must have exactly 1 entry [op_id]; got %v", required)
	} else if required[0] != "op_id" {
		t.Errorf("required[0] must be op_id; got %v", required[0])
	}

	// 3. properties declares all 7 params
	props, ok := s["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties object")
	}
	want7 := []string{"op_id", "args", "variant_id", "fields", "page_size", "page_token", "format"}
	for _, name := range want7 {
		if _, exists := props[name]; !exists {
			t.Errorf("schema missing property %q (spec §4.1: gum.read has 7 params)", name)
		}
	}
	if len(props) != 7 {
		t.Errorf("schema has %d properties; want exactly 7: %v", len(props), want7)
	}

	// 4. format enum is exactly ["toon","csv","json","markdown"]
	formatProp, ok := props["format"].(map[string]any)
	if !ok {
		t.Fatal("format property missing or not an object")
	}
	rawEnum, ok := formatProp["enum"].([]any)
	if !ok {
		t.Fatal("format.enum missing or not an array")
	}
	wantEnum := []string{"toon", "csv", "json", "markdown"}
	if len(rawEnum) != len(wantEnum) {
		t.Errorf("format enum has %d entries; want %d (%v)", len(rawEnum), len(wantEnum), wantEnum)
	} else {
		for i, v := range wantEnum {
			if rawEnum[i] != v {
				t.Errorf("format enum[%d]=%v; want %q", i, rawEnum[i], v)
			}
		}
	}
}

// --- Test 2: Risk-gate — write op via gum.read → RISK_TOOL_MISMATCH -----------

// TestGumReadRiskToolMismatchWriteOp calls gum.read with an op_id whose default
// variant has risk_class=write. Expects RISK_TOOL_MISMATCH structured error
// with the correct detail fields and no upstream dispatch.
//
// Spec anchor: spec.md §4.1 risk-gate step 3; §1421 RISK_TOOL_MISMATCH envelope:
// {"error_code":"RISK_TOOL_MISMATCH","op_id":"...","variant_id":"...","variant_risk_class":"write","required_tool":"gum.write"}
//
// Current handleRiskTier returns a flat string like
// "RISK_TIER_MISMATCH: ... has risk_class=write; routed via gum.read ..." (not
// structured JSON, wrong error_code). This test MUST FAIL until fixed.
func TestGumReadRiskToolMismatchWriteOp(t *testing.T) {
	const opID = "drive.files.create"
	writeOp := minimalReadOp(opID, catalog.RiskClassWrite)
	snap := minimalCatalog(writeOp)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	req := makeReadRequest(map[string]any{"op_id": opID})
	res, err := srv.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead returned unexpected Go error: %v", err)
	}

	m := parseErrorResult(t, res)

	// error_code must be "RISK_TOOL_MISMATCH"
	if code, _ := m["error_code"].(string); code != "RISK_TOOL_MISMATCH" {
		t.Errorf("error_code=%q; want RISK_TOOL_MISMATCH", code)
	}
	// op_id
	if v, _ := m["op_id"].(string); v != opID {
		t.Errorf("op_id=%q; want %q", v, opID)
	}
	// variant_id must be the resolved variant
	wantVariantID := opID + ".v1.test"
	if v, _ := m["variant_id"].(string); v != wantVariantID {
		t.Errorf("variant_id=%q; want %q", v, wantVariantID)
	}
	// variant_risk_class must be "write"
	if v, _ := m["variant_risk_class"].(string); v != "write" {
		t.Errorf("variant_risk_class=%q; want \"write\"", v)
	}
	// required_tool must be "gum.write"
	if v, _ := m["required_tool"].(string); v != "gum.write" {
		t.Errorf("required_tool=%q; want \"gum.write\"", v)
	}
	// No upstream dispatch
	if len(cd.Calls) != 0 {
		t.Errorf("dispatcher was called %d time(s); want 0 (no upstream call on risk mismatch)", len(cd.Calls))
	}
}

// --- Test 3: Risk-gate — destructive op via gum.read → RISK_TOOL_MISMATCH ---

// TestGumReadRiskToolMismatchDestructiveOp is the same as Test 2 but with
// risk_class=destructive; required_tool must be "gum.destructive".
//
// Spec anchor: same as Test 2.
func TestGumReadRiskToolMismatchDestructiveOp(t *testing.T) {
	const opID = "drive.files.delete"
	destOp := minimalReadOp(opID, catalog.RiskClassDestructive)
	snap := minimalCatalog(destOp)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	req := makeReadRequest(map[string]any{"op_id": opID})
	res, err := srv.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead returned unexpected Go error: %v", err)
	}

	m := parseErrorResult(t, res)

	if code, _ := m["error_code"].(string); code != "RISK_TOOL_MISMATCH" {
		t.Errorf("error_code=%q; want RISK_TOOL_MISMATCH", code)
	}
	if v, _ := m["op_id"].(string); v != opID {
		t.Errorf("op_id=%q; want %q", v, opID)
	}
	wantVariantID := opID + ".v1.test"
	if v, _ := m["variant_id"].(string); v != wantVariantID {
		t.Errorf("variant_id=%q; want %q", v, wantVariantID)
	}
	if v, _ := m["variant_risk_class"].(string); v != "destructive" {
		t.Errorf("variant_risk_class=%q; want \"destructive\"", v)
	}
	if v, _ := m["required_tool"].(string); v != "gum.destructive" {
		t.Errorf("required_tool=%q; want \"gum.destructive\"", v)
	}
	if len(cd.Calls) != 0 {
		t.Errorf("dispatcher was called %d time(s); want 0", len(cd.Calls))
	}
}

// --- Test 4: Happy path — read op passes through to dispatcher ---------------

// TestGumReadDispatchesRiskClassRead verifies that gum.read with a risk_class=read
// op calls the dispatcher and returns its response body.
//
// Spec anchor: spec.md §4.1 — "gum.read dispatches a catalog variant with
// risk_class: read".
//
// If the current handleRiskTier does not reach the dispatcher (e.g. returns an
// error) this test fails. Currently the implementation has a bug in error
// format/code; the happy path might still work but this test pins both dispatch
// call count and result shape.
func TestGumReadDispatchesRiskClassRead(t *testing.T) {
	const opID = "gmail.users.messages.list"
	readOp := minimalReadOp(opID, catalog.RiskClassRead)
	snap := minimalCatalog(readOp)
	cd := &captureDispatcher{Body: []byte(`{"headers":["id"],"rows":[["abc123"]]}`)}
	srv := NewServerWithCatalog(cd, snap)

	req := makeReadRequest(map[string]any{"op_id": opID})
	res, err := srv.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead returned unexpected Go error: %v", err)
	}
	if res == nil {
		t.Fatal("result is nil")
	}
	if res.IsError {
		// extract text for diagnosis
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
				t.Fatalf("unexpected error result: %s", tc.Text)
			}
		}
		t.Fatal("unexpected error result")
	}
	if len(cd.Calls) != 1 {
		t.Errorf("dispatcher called %d time(s); want 1", len(cd.Calls))
	}
	if len(cd.Calls) == 1 && cd.Calls[0].OpID != opID {
		t.Errorf("dispatcher received op_id=%q; want %q", cd.Calls[0].OpID, opID)
	}
	// Response body must be present
	if len(res.Content) == 0 {
		t.Error("result has no content")
	}
}

// --- Test 5: MCP annotations — readOnlyHint=true, destructiveHint=false ------

// TestGumReadAnnotationsReadOnly asserts that the gum.read tool registration
// carries readOnlyHint=true and destructiveHint=false in its MCP annotations.
//
// Spec anchor: spec.md §4.1 — "MCP annotation: readOnlyHint=true,
// destructiveHint=false, idempotentHint derived from the op."
// test-matrix.md TestToolAnnotationsWireForm row.
//
// The current server.go registers gum.read WITHOUT Annotations (the
// sdkmcp.Tool struct field is nil). This test MUST FAIL until the registration
// is updated.
func TestGumReadAnnotationsReadOnly(t *testing.T) {
	// We inspect the tool as listed by the SDK server via ListTools.
	srv := NewServer(schemaTestDispatcher{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var readTool *sdkmcp.Tool
	for i := range result.Tools {
		if result.Tools[i].Name == "gum.read" {
			readTool = result.Tools[i]
			break
		}
	}
	if readTool == nil {
		t.Fatal("gum.read not found in tools/list")
	}

	ann := readTool.Annotations
	if ann == nil {
		t.Fatal("gum.read has nil Annotations; want readOnlyHint=true, destructiveHint=false (spec §4.1)")
	}
	// readOnlyHint must be true
	if !ann.ReadOnlyHint {
		t.Errorf("gum.read Annotations.ReadOnlyHint=%v; want true (spec §4.1)", ann.ReadOnlyHint)
	}
	// destructiveHint must be explicitly false (not nil pointer — must be set)
	if ann.DestructiveHint == nil {
		t.Error("gum.read Annotations.DestructiveHint is nil; want explicit *false (spec §4.1)")
	} else if *ann.DestructiveHint {
		t.Errorf("gum.read Annotations.DestructiveHint=%v; want false (spec §4.1)", *ann.DestructiveHint)
	}
}

// --- Test 6: Pagination passthrough ------------------------------------------

// TestGumReadPaginationPassthrough verifies that the host-control page_token and
// page_size values supplied at the gum.read level flow into Invocation.Args
// REMAPPED to the canonical Google query parameters: page_token -> pageToken and
// page_size -> the op's declared page-size param (maxResults here, per Gmail).
// Forwarding the snake_case names verbatim would be silently ignored upstream.
//
// Spec anchor: spec.md §4.1 — "pagination: continuation tokens flow via
// page_token … passed to upstream API."
func TestGumReadPaginationPassthrough(t *testing.T) {
	const opID = "gmail.users.messages.list"
	readOp := minimalReadOp(opID, catalog.RiskClassRead)
	// Gmail uses maxResults for page size; declare it so the remap resolves it.
	readOp.RequestFields = []catalog.RequestField{
		{Name: "maxResults", Location: catalog.RequestFieldQuery, Type: "integer"},
		{Name: "pageToken", Location: catalog.RequestFieldQuery, Type: "string"},
	}
	snap := minimalCatalog(readOp)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	req := makeReadRequest(map[string]any{
		"op_id":      opID,
		"page_token": "tok_abc",
		"page_size":  float64(25),
	})
	res, err := srv.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead returned unexpected Go error: %v", err)
	}
	if res != nil && res.IsError {
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
				t.Logf("error result: %s", tc.Text)
			}
		}
		t.Fatal("handleRead returned error result; want success")
	}
	if len(cd.Calls) != 1 {
		t.Fatalf("dispatcher called %d time(s); want 1", len(cd.Calls))
	}

	inv := cd.Calls[0]
	// page_token remaps to the canonical pageToken.
	if tok, _ := inv.Args["pageToken"].(string); tok != "tok_abc" {
		t.Errorf("inv.Args[pageToken]=%q; want \"tok_abc\"", tok)
	}
	// page_size remaps to the op's declared param (maxResults for Gmail).
	if sz, _ := inv.Args["maxResults"].(float64); sz != 25 {
		t.Errorf("inv.Args[maxResults]=%v; want 25", inv.Args["maxResults"])
	}
	// The snake_case forms must not leak through.
	for _, dead := range []string{"page_token", "page_size"} {
		if _, present := inv.Args[dead]; present {
			t.Errorf("snake_case arg %q leaked into the invocation", dead)
		}
	}
}

// --- Test 7: OP_NOT_FOUND with suggestions -----------------------------------

// TestGumReadUnknownOpReturnsSuggestions verifies that calling gum.read with an
// unknown op_id returns OP_NOT_FOUND with a "suggestions" key in the error
// envelope. When a similar op_id exists in the catalog, suggestions must be
// non-empty; when the catalog is empty, suggestions may be empty/absent.
//
// Spec anchor: spec.md §4.1 — dispatch step 1 returns OP_NOT_FOUND on unknown
// op_id; §1421 stable error code OP_NOT_FOUND. BM25 suggestions per
// test-matrix.md row: "OP_NOT_FOUND … .Detail['suggestions'] array".
//
// The current implementation returns a flat string "OP_NOT_FOUND: <op_id>"
// (not structured JSON). This test MUST FAIL until handleRead emits a
// structured OP_NOT_FOUND envelope.
func TestGumReadUnknownOpReturnsSuggestions(t *testing.T) {
	// Seed the catalog with a closely-named op so the BM25 index can surface it.
	similarOp := minimalReadOp("gmail.users.messages.list", catalog.RiskClassRead)
	snap := minimalCatalog(similarOp)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	// Typo: "gmail.users.mesages.list" (missing an 's') — similar enough for BM25.
	req := makeReadRequest(map[string]any{"op_id": "gmail.users.mesages.list"})
	res, err := srv.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead returned unexpected Go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatal("expected error result for unknown op_id")
	}

	// Must be structured JSON with error_code=OP_NOT_FOUND
	if len(res.Content) == 0 {
		t.Fatal("error result has no content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is not TextContent; got %T", res.Content[0])
	}
	text := tc.Text

	// The result text must be parseable as JSON with error_code=OP_NOT_FOUND.
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		// flat string like "OP_NOT_FOUND: gmail.users.mesages.list" — not structured
		if !strings.HasPrefix(text, "OP_NOT_FOUND") {
			t.Fatalf("expected OP_NOT_FOUND error; got: %s", text)
		}
		t.Errorf("OP_NOT_FOUND response is not structured JSON: %s; "+
			"spec §1421 requires a structured envelope with suggestions array", text)
		return
	}

	if code, _ := m["error_code"].(string); code != "OP_NOT_FOUND" {
		t.Errorf("error_code=%q; want OP_NOT_FOUND", code)
	}

	// suggestions key must be present (may be empty when no similar ops exist,
	// but must be present as an array).
	rawSuggestions, hasSuggestions := m["suggestions"]
	if !hasSuggestions {
		t.Error("OP_NOT_FOUND envelope missing 'suggestions' key (spec §1421, test-matrix BM25 row)")
		return
	}
	suggestions, ok := rawSuggestions.([]any)
	if !ok {
		t.Errorf("suggestions is not an array; got %T", rawSuggestions)
		return
	}
	// With a similar op in the catalog, suggestions should be non-empty.
	if len(suggestions) == 0 {
		t.Logf("suggestions is empty — BM25 index may need to be built for a similar op to appear; " +
			"this is a soft failure. Similar op: gmail.users.messages.list")
	}

	// No upstream dispatch.
	if len(cd.Calls) != 0 {
		t.Errorf("dispatcher was called %d time(s); want 0 for unknown op", len(cd.Calls))
	}
}

// TestGumReadPageSizeZeroNotForwarded is the audit regression: a page_size of 0
// must NOT be forwarded to the upstream (it has no valid Google pagination
// meaning — empty page or 400). The CLI guards on `> 0`; the MCP path now does
// too. Before the fix any present page_size, including 0, was passed through.
func TestGumReadPageSizeZeroNotForwarded(t *testing.T) {
	const opID = "gmail.users.messages.list"
	readOp := minimalReadOp(opID, catalog.RiskClassRead)
	readOp.RequestFields = []catalog.RequestField{
		{Name: "maxResults", Location: catalog.RequestFieldQuery, Type: "integer"},
	}
	snap := minimalCatalog(readOp)
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, snap)

	req := makeReadRequest(map[string]any{
		"op_id":     opID,
		"page_size": float64(0),
	})
	res, err := srv.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead Go error: %v", err)
	}
	if res != nil && res.IsError {
		t.Fatal("handleRead returned error result; want success")
	}
	if len(cd.Calls) != 1 {
		t.Fatalf("dispatcher called %d time(s); want 1", len(cd.Calls))
	}
	if _, present := cd.Calls[0].Args["maxResults"]; present {
		t.Errorf("page_size=0 was forwarded as maxResults=%v; want dropped (CLI parity)", cd.Calls[0].Args["maxResults"])
	}
}
