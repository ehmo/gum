package mcp

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/gain"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// buildReq is a tiny helper that builds a CallToolRequest with the
// given args map JSON-encoded into Params.Arguments.
func buildReq(t *testing.T, args map[string]any) *sdkmcp.CallToolRequest {
	t.Helper()
	a, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Arguments: a},
	}
}

// errBodyOf returns the text body of an error result for assertion.
func errBodyOf(t *testing.T, res *sdkmcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("nil/empty result content")
	}
	text, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0]=%T; want *TextContent", res.Content[0])
	}
	return text.Text
}

// TestHandleDescribeOpEmptyOpIDReturnsInvalidArgs pins handleDescribeOp's
// `opID == "" → INVALID_ARGS` arm (handlers.go:175-180). A request
// without an op_id MUST be rejected before catalog lookup so callers
// can't get an OP_NOT_FOUND-with-empty-suggestions response that hides
// the real bug (forgot to pass op_id).
func TestHandleDescribeOpEmptyOpIDReturnsInvalidArgs(t *testing.T) {
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
	req := buildReq(t, map[string]any{}) // no op_id
	res, err := s.handleDescribeOp(context.Background(), req)
	if err != nil {
		t.Fatalf("handleDescribeOp: %v", err)
	}
	body := errBodyOf(t, res)
	if !strings.Contains(body, "INVALID_ARGS") {
		t.Errorf("body=%q; want INVALID_ARGS", body)
	}
	if !strings.Contains(body, "op_id is required") {
		t.Errorf("body=%q; want 'op_id is required'", body)
	}
}

// TestHandleRiskTierEmptyOpIDReturnsInvalidArgs pins handleRiskTier's
// `opID == "" → INVALID_ARGS` arm (handlers.go:211-213). Identical
// guard at the gum.read/write/destructive entry; reached via handleRead.
func TestHandleRiskTierEmptyOpIDReturnsInvalidArgs(t *testing.T) {
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
	req := buildReq(t, map[string]any{}) // no op_id
	res, err := s.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead: %v", err)
	}
	body := errBodyOf(t, res)
	if !strings.Contains(body, "INVALID_ARGS") {
		t.Errorf("body=%q; want INVALID_ARGS", body)
	}
}

// TestHandleSearchAPIsEmptyQueryReturnsInvalidArgs pins
// handleSearchAPIs's `query == "" → INVALID_ARGS` arm (handlers.go:95-97).
// A blank query against BM25 returns the whole corpus; that's a footgun
// for token budgets, so the handler rejects it upfront.
func TestHandleSearchAPIsEmptyQueryReturnsInvalidArgs(t *testing.T) {
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
	req := buildReq(t, map[string]any{}) // no query
	res, err := s.handleSearchAPIs(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSearchAPIs: %v", err)
	}
	body := errBodyOf(t, res)
	if !strings.Contains(body, "INVALID_ARGS") {
		t.Errorf("body=%q; want INVALID_ARGS", body)
	}
	if !strings.Contains(body, "query is required") {
		t.Errorf("body=%q; want 'query is required'", body)
	}
}

// TestAuditBrokenEmptyProfileDefaultsToDefault pins auditBroken's
// `profile == "" → profile = "default"` arm (handlers.go:392-394).
// Reached when the server was constructed without an explicit profile
// name (most v0.1.0 deployments). Without the default, the path would
// be `<dataHome>/gum//audit.broken` (double slash) and the sentinel
// check would silently fail.
//
// We assert behavior by planting a sentinel at the default path and
// checking auditBroken() returns true.
func TestAuditBrokenEmptyProfileDefaultsToDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// Plant the sentinel at the "default" profile path.
	dir := tmp + "/gum/default"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dir+"/audit.broken", []byte("1"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// Construct a Server WITHOUT setting profile — should default to "default".
	s := &Server{}
	if !s.auditBroken() {
		t.Errorf("auditBroken()=false; want true (empty profile must resolve to 'default' path)")
	}
}

// TestAuditBrokenHomeUnavailableReturnsFalse pins auditBroken's
// `UserHomeDir err → return false` arm (handlers.go:398-400). With
// XDG_DATA_HOME unset AND HOME unset, UserHomeDir fails — auditBroken
// MUST surface "no broken state" rather than crashing or returning
// true. This is the well-defined safe default for a misconfigured host.
//
// Skipped on Windows: UserHomeDir uses USERPROFILE, not HOME.
func TestAuditBrokenHomeUnavailableReturnsFalse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir on Windows uses USERPROFILE, not HOME")
	}
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")

	s := &Server{profile: "default"}
	if s.auditBroken() {
		t.Errorf("auditBroken()=true; want false on UserHomeDir err (safe default)")
	}
}

// TestGainSuccessEnvelopeNonZeroSavingsComputesPct pins gainSuccessEnvelope's
// reporting of the REAL token figures: baseline_tokens = the raw-token total
// (TotalTokensIn), actual_tokens = baseline - savings (the shaped total), and
// savings_pct = the real reduction — not the old fake "always 100%".
func TestGainSuccessEnvelopeNonZeroSavingsComputesPct(t *testing.T) {
	// Realistic stats: 1000 raw tokens in, 850 saved → 150 shaped, 85% reduction.
	// baseline_tokens is the raw total (TotalTokensIn), NOT the savings (the old
	// approximation reported baseline==savings → actual=0, savings_pct=100% always).
	got := gainSuccessEnvelope(gain.Stats{TotalTokensIn: 1000, TotalTokensSaved: 850})
	if got["baseline_tokens"] != int64(1000) {
		t.Errorf("baseline_tokens=%v; want 1000 (the raw-token total)", got["baseline_tokens"])
	}
	if got["actual_tokens"] != int64(150) {
		t.Errorf("actual_tokens=%v; want 150 (baseline - savings = shaped total)", got["actual_tokens"])
	}
	if got["savings_tokens"] != int64(850) {
		t.Errorf("savings_tokens=%v; want 850", got["savings_tokens"])
	}
	pct, ok := got["savings_pct"].(float64)
	if !ok {
		t.Fatalf("savings_pct type=%T; want float64 (computed when baseline>0)", got["savings_pct"])
	}
	if pct != 85.0 {
		t.Errorf("savings_pct=%v; want 85.0 (real reduction, not a fake 100%%)", pct)
	}
}

// TestGainSuccessEnvelopeZeroSavingsLeavesPctNil pins the inverse:
// when no savings recorded, baseline stays 0 and savings_pct is nil
// (skips the divide-by-zero risk). Counterpart to the non-zero arm.
func TestGainSuccessEnvelopeZeroSavingsLeavesPctNil(t *testing.T) {
	got := gainSuccessEnvelope(gain.Stats{TotalTokensSaved: 0})
	if got["savings_pct"] != nil {
		t.Errorf("savings_pct=%v; want nil (zero baseline must NOT divide)", got["savings_pct"])
	}
	if got["baseline_tokens"] != int64(0) {
		t.Errorf("baseline_tokens=%v; want 0", got["baseline_tokens"])
	}
}

// TestApplyRiskFlagsFromCatalogVariantNilNoOps pins
// applyRiskFlagsFromCatalog's `v == nil → return` arm (handlers.go:520-522).
// An op with no default variant (mis-built catalog) must NOT mutate
// inv's flags — silently returning is safer than panicking on the
// switch over a nil variant's RiskClass.
func TestApplyRiskFlagsFromCatalogVariantNilNoOps(t *testing.T) {
	// Op with no variants → defaultVariant returns nil.
	op := catalog.Op{
		OpID:             "broken.op",
		DefaultVariantID: "v.1",
		Variants:         nil, // empty
	}
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
		Ops:                  []catalog.Op{op},
	})
	inv := &dispatch.Invocation{OpID: "broken.op"}
	s.applyRiskFlagsFromCatalog(inv)
	if inv.AllowWrite || inv.AllowDestructive {
		t.Errorf("inv flags mutated: AllowWrite=%v AllowDestructive=%v; want both false (nil variant path)",
			inv.AllowWrite, inv.AllowDestructive)
	}
}

// TestApplyRiskFlagsFromCatalogDestructiveSetsAllowDestructive pins
// the destructive case (handlers.go:526-527). When the catalog variant's
// risk_class is destructive, the inv's AllowDestructive flag MUST be
// pre-set so downstream policy gates don't reject the dispatch.
func TestApplyRiskFlagsFromCatalogDestructiveSetsAllowDestructive(t *testing.T) {
	op := catalog.Op{
		OpID:             "destructive.op",
		DefaultVariantID: "v.1",
		Variants: []catalog.Variant{
			{
				VariantID:            "v.1",
				VariantSchemaVersion: 1,
				RiskClass:            catalog.RiskClassDestructive,
			},
		},
	}
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
		Ops:                  []catalog.Op{op},
	})
	inv := &dispatch.Invocation{OpID: "destructive.op"}
	s.applyRiskFlagsFromCatalog(inv)
	if !inv.AllowDestructive {
		t.Errorf("AllowDestructive=false; want true (destructive variant)")
	}
}

// TestHandleUnknownReturnsMetaToolNotImplemented pins handleUnknown's
// returned handler (handlers.go:484-489). Any unregistered meta-tool
// name flows through this path; the response MUST be a stable
// META_TOOL_NOT_IMPLEMENTED envelope so clients can detect the
// mismatch and version-pin.
func TestHandleUnknownReturnsMetaToolNotImplemented(t *testing.T) {
	s := NewServerWithCatalog(noopDispatcher{}, &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratorVersion:     "test",
	})
	handler := s.handleUnknown("gum.unknown_tool")
	res, err := handler(context.Background(), buildReq(t, nil))
	if err != nil {
		t.Fatalf("handleUnknown: %v", err)
	}
	body := errBodyOf(t, res)
	if !strings.Contains(body, "META_TOOL_NOT_IMPLEMENTED") {
		t.Errorf("body=%q; want META_TOOL_NOT_IMPLEMENTED", body)
	}
	if !strings.Contains(body, "gum.unknown_tool") {
		t.Errorf("body=%q; want tool name in message", body)
	}
}
