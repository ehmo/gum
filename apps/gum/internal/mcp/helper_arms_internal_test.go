package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// TestNumericArgCoercions pins the non-float64 arms of numericArg. A
// programmatic caller (CLI bridge, test harness) can hand any Go numeric
// type through, so each must coerce rather than fall to the default arm.
func TestNumericArgCoercions(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want float64
		ok   bool
	}{
		{"float64", float64(3), 3, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", int(7), 7, true},
		{"int64", int64(9), 9, true},
		{"json_number", json.Number("11"), 11, true},
		{"bad_json_number", json.Number("not-a-number"), 0, false},
		{"string", "5", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := numericArg(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok=%v; want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("value=%v; want %v", got, tc.want)
			}
		})
	}
}

// TestSetProfileRejectsInvalidName pins the parse arm of SetProfile. The
// profile name becomes a directory component, so a name carrying a separator
// must be refused instead of escaping the data dir.
func TestSetProfileRejectsInvalidName(t *testing.T) {
	s := &Server{profile: "default"}
	if err := s.SetProfile("bad/name"); err == nil {
		t.Fatal("SetProfile err=nil; want a parse failure")
	}
	if s.profile != "default" {
		t.Errorf("profile=%q; want the previous value kept", s.profile)
	}
}

// TestSessionsExposesSDKSessions pins the Sessions accessor. A freshly built
// server has no connected client, so the sequence is empty.
func TestSessionsExposesSDKSessions(t *testing.T) {
	s := NewServerWithCatalog(nil, &catalog.Catalog{})
	var count int
	for range s.Sessions() {
		count++
	}
	if count != 0 {
		t.Errorf("sessions=%d; want 0 before any client connects", count)
	}
}

// TestToolTextFallbacks pins the default arms of the description and schema
// tables. An unregistered name must still yield a usable string rather than
// an empty one.
func TestToolTextFallbacks(t *testing.T) {
	if got := metaToolDescription("gum.not_a_tool"); got != "gum meta-tool" {
		t.Errorf("metaToolDescription=%q; want the fallback", got)
	}
	if got := convenienceToolDescription("not_a_tool"); got != "gum convenience tool" {
		t.Errorf("convenienceToolDescription=%q; want the fallback", got)
	}
	if got := string(metaToolSchema("gum.not_a_tool")); !strings.Contains(got, `"additionalProperties":false`) {
		t.Errorf("metaToolSchema=%q; want the closed-object fallback", got)
	}
	if got := string(metaToolOutputSchema("gum.not_a_tool")); !strings.Contains(got, "SingleObjectResult") {
		t.Errorf("metaToolOutputSchema=%q; want the single-object fallback", got)
	}
}

// TestNilShapedHelpers pins the nil guards of the result-shape helpers. Both
// run on the MCP response path, where a refused dispatch leaves no shaped
// response to read.
func TestNilShapedHelpers(t *testing.T) {
	if got := tierAResult(nil); got != nil {
		t.Errorf("tierAResult(nil)=%v; want nil", got)
	}
	if got := onEmptyMessageOf(&dispatch.ShapedResponse{}); got != "" {
		t.Errorf("onEmptyMessageOf=%q; want empty without an expression envelope", got)
	}
}

// TestClientCapabilityGuards pins the nil-request arms of the two per-call
// client probes. Both run for CLI-side dispatch, where there is no MCP
// request to inspect.
func TestClientCapabilityGuards(t *testing.T) {
	if clientSupportsPromptCache(nil) {
		t.Error("clientSupportsPromptCache(nil)=true; want false")
	}
	if got := rootsFromInputResponses(&sdkmcp.CallToolRequest{}); got != nil {
		t.Errorf("rootsFromInputResponses=%v; want nil without params", got)
	}
}

// TestResolveInputSchemaRejectsMalformedSchema pins the parse arm of the
// registration-time schema compiler.
func TestResolveInputSchemaRejectsMalformedSchema(t *testing.T) {
	_, err := resolveInputSchema(json.RawMessage(`{"type":`))
	if err == nil {
		t.Fatal("resolveInputSchema err=nil; want a parse failure")
	}
	if !strings.Contains(err.Error(), "parse:") {
		t.Errorf("err=%v; want the parse arm", err)
	}
}

// TestValidateToolArgsRejectsMalformedArguments pins the arguments-decode arm.
// The SDK hands raw bytes through, so a truncated payload must be refused at
// the validation seam rather than reaching a handler.
func TestValidateToolArgsRejectsMalformedArguments(t *testing.T) {
	resolved, err := resolveInputSchema(json.RawMessage(`{"type":"object"}`))
	if err != nil {
		t.Fatalf("resolveInputSchema: %v", err)
	}
	err = validateToolArgs(resolved, json.RawMessage(`{"a":`))
	if err == nil {
		t.Fatal("validateToolArgs err=nil; want the decode failure")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("err=%v; want the decode arm", err)
	}
}

// TestHelpTopicNameAcceptsDigitsAndDashes pins the digit and dash arms of the
// topic-name validator, and the rejection of anything else.
func TestHelpTopicNameAcceptsDigitsAndDashes(t *testing.T) {
	name, ok := parseHelpTopicURI(helpTopicURIPrefix + "auth-2fa")
	if !ok || name != "auth-2fa" {
		t.Fatalf("parseHelpTopicURI=%q,%v; want auth-2fa,true", name, ok)
	}
	if _, ok := parseHelpTopicURI(helpTopicURIPrefix + "Auth"); ok {
		t.Error("uppercase topic accepted; want rejection")
	}
}

// TestCanonicalPageSizeParamArms pins the three page-size mappings. Google
// REST ops are split between pageSize and maxResults, and an op the snapshot
// does not carry must still yield a usable default.
func TestCanonicalPageSizeParamArms(t *testing.T) {
	s := &Server{snapshot: &catalog.Catalog{Ops: []catalog.Op{
		{OpID: "drive.list", RequestFields: []catalog.RequestField{{Name: "pageSize"}}},
		{OpID: "gmail.list", RequestFields: []catalog.RequestField{{Name: "maxResults"}}},
		{OpID: "other.get", RequestFields: []catalog.RequestField{{Name: "id"}}},
	}}}

	cases := map[string]string{
		"drive.list":   "pageSize",
		"gmail.list":   "maxResults",
		"other.get":    "pageSize",
		"absent.op_id": "pageSize",
	}
	for opID, want := range cases {
		if got := s.canonicalPageSizeParam(opID); got != want {
			t.Errorf("canonicalPageSizeParam(%q)=%q; want %q", opID, got, want)
		}
	}
}

// TestGateVariantPinArms pins the two nil results of gateVariant. Both mean
// "no gate here"; the kernel refuses the call with VARIANT_QUARANTINED or
// VARIANT_NOT_FOUND before anything executes.
func TestGateVariantPinArms(t *testing.T) {
	op := &catalog.Op{
		OpID:             "drive.list",
		DefaultVariantID: "v1",
		Variants: []catalog.Variant{
			{VariantID: "v1"},
			{VariantID: "quarantined", Quarantined: true},
		},
	}
	if got := gateVariant(op, "quarantined"); got != nil {
		t.Errorf("gateVariant(quarantined)=%v; want nil", got)
	}
	if got := gateVariant(op, "absent"); got != nil {
		t.Errorf("gateVariant(absent)=%v; want nil", got)
	}
	if got := gateVariant(op, "v1"); got == nil || got.VariantID != "v1" {
		t.Errorf("gateVariant(v1)=%v; want the pinned variant", got)
	}
}

// TestApplyRiskFlagsUnknownOpIsNoOp pins the miss arm: an invocation naming an
// op the snapshot does not carry must leave the risk flags untouched rather
// than invent them.
func TestApplyRiskFlagsUnknownOpIsNoOp(t *testing.T) {
	s := &Server{snapshot: &catalog.Catalog{}}
	inv := &dispatch.Invocation{OpID: "absent.op_id"}
	s.applyRiskFlagsFromCatalog(inv)
	if inv.AllowWrite || inv.AllowDestructive {
		t.Errorf("AllowWrite=%v AllowDestructive=%v; want both left false", inv.AllowWrite, inv.AllowDestructive)
	}
}

// TestBuildDescribeOpResultFallbackVariant pins the defensive first-variant
// fallback and the nil-to-empty conversion §13 requires on a non-full
// execution_support branch.
func TestBuildDescribeOpResultFallbackVariant(t *testing.T) {
	op := &catalog.Op{
		OpID:             "drive.list",
		DefaultVariantID: "missing",
		Variants: []catalog.Variant{{
			VariantID:        "v1",
			ExecutionSupport: catalog.ExecutionSupportPartial,
		}},
	}
	res := buildDescribeOpResult(op, 10)
	if res.UnsupportedCapabilities == nil {
		t.Fatal("UnsupportedCapabilities=nil; want an empty list on a partial variant")
	}
	if len(*res.UnsupportedCapabilities) != 0 {
		t.Errorf("UnsupportedCapabilities=%v; want empty", *res.UnsupportedCapabilities)
	}
	if res.ExecutionSupport != string(catalog.ExecutionSupportPartial) {
		t.Errorf("ExecutionSupport=%q; want partial", res.ExecutionSupport)
	}
}
