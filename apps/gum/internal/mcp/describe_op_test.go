// Package mcp — Red Team failing tests for gum-9vuq.2.
//
// These tests assert the acceptance criteria for issue gum-9vuq.2:
// gum.describe_op MUST return a compact structured result (DescribeOpResult)
// as specified in spec.md §4.1 and the #/$defs/DescribeOpResult schema at
// spec.md §Appendix-A / §9.4.
//
// Test matrix row: TestDescribeOpOutputSchema (test-matrix.md line 98)
//
// Spec anchors:
//   - spec.md §4.1 — gum.describe_op compact metadata shape
//   - spec.md §5.1.2 — risk_override provenance fields
//   - spec.md Appendix A §DescribeOpResult — JSON Schema definition
//   - test-matrix.md: TestDescribeOpOutputSchema
//
// Required exports the Green Team MUST add (currently missing):
//
//	func describeOp(s *Server, opID string) (map[string]any, error)
//	    — or — the handler itself must produce the compact DescribeOpResult shape
//
// The current handleDescribeOp returns the raw catalog.Op struct which does NOT
// match the compact shape. These tests will fail until the compact shape is implemented.
//
// Green Team implementation hint: add internal/mcp/describe_op.go containing:
//   - type describeOpResult struct { ... }
//   - func buildDescribeOpResult(op *catalog.Op, maxVariants int) describeOpResult
// The handler calls buildDescribeOpResult and returns jsonResult(result).
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// ---- helpers ----------------------------------------------------------------

// describeOpDispatcher is a no-op dispatcher for describe_op tests (describe_op
// does not call dispatch.Dispatch).
type describeOpDispatcher struct{}

func (describeOpDispatcher) Dispatch(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	panic("describeOpDispatcher.Dispatch must not be called in describe_op tests")
}

// callDescribeOp calls the gum.describe_op handler directly on a server
// constructed with the given catalog snapshot and returns the parsed JSON body.
func callDescribeOp(t *testing.T, snap *catalog.Catalog, opID string) map[string]any {
	t.Helper()
	s := NewServerWithCatalog(describeOpDispatcher{}, snap)

	args, _ := json.Marshal(map[string]string{"op_id": opID})
	req := &sdkmcp.CallToolRequest{}
	req.Params = &sdkmcp.CallToolParamsRaw{
		Arguments: args,
	}

	res, err := s.handleDescribeOp(context.Background(), req)
	if err != nil {
		t.Fatalf("handleDescribeOp returned unexpected Go error: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("handleDescribeOp returned nil/empty result for op_id=%q", opID)
	}

	text, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("handleDescribeOp content[0] is not *TextContent")
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatalf("handleDescribeOp body is not JSON: %v\nbody: %s", err, text.Text)
	}
	return out
}

// makeMinimalOp builds the smallest valid catalog.Op with n variants.
// variant IDs are "v.1", "v.2", ... "v.n"; default is "v.1".
func makeMinimalOp(opID string, n int) catalog.Op {
	variants := make([]catalog.Variant, n)
	for i := range n {
		variants[i] = catalog.Variant{
			VariantID:        "v." + itoa(i+1),
			VariantSchemaVersion: 1,
			Stability:        catalog.StabilityStable,
			InterfaceKind:    catalog.InterfaceKindDiscoveryREST,
			BackendKind:      catalog.BackendKindDiscoveryREST,
			RiskClass:        catalog.RiskClassRead,
			Scopes:           []string{"https://www.googleapis.com/auth/gmail.readonly"},
			OutputProfile:    "gmail.messages.list.v1",
			ExecutionSupport: "full",
		}
	}
	return catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            "Test Op",
		Summary:          "A test operation.",
		DefaultVariantID: "v.1",
		Variants:         variants,
	}
}

func makeMinimalCatalog(ops ...catalog.Op) *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  ops,
	}
}

// itoa converts a small non-negative int to a decimal string.
func itoa(n int) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ---- Test 1: happy-path compact metadata ------------------------------------

// TestDescribeOpReturnsCompactMetadata verifies that gum.describe_op returns the
// compact DescribeOpResult shape for a single-variant op, not the raw catalog.Op.
//
// Spec anchor: spec.md §4.1 — "returns a compact active-snapshot selector summary:
// default variant, ordered alternate variants, risk class, required scopes, output
// profile, execution-support, schema refs"
//
// Assertions (each must hold for GREEN):
//   - "default_variant_id" key is present and equals "v.1"
//   - "variants" key is a JSON array
//   - "risk_class" key is present and equals "read"
//   - "scopes" key is a JSON array containing the scope string
//   - "output_profile" key is present
//   - "execution_support" key is present
//   - "schema_refs" key is an object (present; may be empty)
//   - "variants_total" key is present and equals 1
//   - "variants_omitted_count" key is present and equals 0
//   - "status" key is NOT present (plugin-only field must be excluded)
//   - "reason" key is NOT present (plugin-only field must be excluded)
//   - raw catalog fields like "op_schema_version" are NOT present in the result
//     (the result is the compact shape, not the raw catalog.Op)
func TestDescribeOpReturnsCompactMetadata(t *testing.T) {
	op := makeMinimalOp("test.op.read", 1)
	snap := makeMinimalCatalog(op)

	got := callDescribeOp(t, snap, "test.op.read")

	// Must have the required compact fields.
	if got["default_variant_id"] != "v.1" {
		t.Errorf("default_variant_id = %v; want %q", got["default_variant_id"], "v.1")
	}

	variants, ok := got["variants"].([]any)
	if !ok {
		t.Errorf("variants is not a JSON array; got %T: %v", got["variants"], got["variants"])
	} else if len(variants) != 1 {
		t.Errorf("variants length = %d; want 1", len(variants))
	}

	if got["risk_class"] != "read" {
		t.Errorf("risk_class = %v; want %q", got["risk_class"], "read")
	}

	scopes, ok := got["scopes"].([]any)
	if !ok {
		t.Errorf("scopes is not a JSON array; got %T: %v", got["scopes"], got["scopes"])
	} else if len(scopes) < 1 {
		t.Errorf("scopes is empty; expected at least 1 scope")
	}

	if _, has := got["output_profile"]; !has {
		t.Errorf("output_profile key is missing from compact result (spec §4.1)")
	}

	if _, has := got["execution_support"]; !has {
		t.Errorf("execution_support key is missing from compact result (spec §4.1)")
	}

	if _, has := got["schema_refs"]; !has {
		t.Errorf("schema_refs key is missing from compact result (spec §4.1 / DescribeOpResult schema)")
	}

	// variants_total and variants_omitted_count are REQUIRED by DescribeOpResult schema.
	if got["variants_total"] == nil {
		t.Errorf("variants_total key is missing; required by spec DescribeOpResult schema")
	}
	if got["variants_omitted_count"] == nil {
		t.Errorf("variants_omitted_count key is missing; required by spec DescribeOpResult schema")
	}
	if vt, ok := got["variants_total"].(float64); !ok || int(vt) != 1 {
		t.Errorf("variants_total = %v; want 1", got["variants_total"])
	}
	if oc, ok := got["variants_omitted_count"].(float64); !ok || int(oc) != 0 {
		t.Errorf("variants_omitted_count = %v; want 0", got["variants_omitted_count"])
	}

	// Plugin-only fields must NOT appear (spec §4.1, DescribeOpResult $comment).
	if _, has := got["status"]; has {
		t.Errorf("status field MUST NOT appear in describe_op result (plugin-only field; spec §9.4 keep-list note)")
	}
	if _, has := got["reason"]; has {
		t.Errorf("reason field MUST NOT appear in describe_op result (plugin-only field; spec §9.4 keep-list note)")
	}

	// The raw catalog field "op_schema_version" must not leak into the compact result.
	if _, has := got["op_schema_version"]; has {
		t.Errorf("op_schema_version is a raw catalog.Op field and must NOT appear in the compact DescribeOpResult; "+
			"current impl returns the raw Op struct — this must be replaced with buildDescribeOpResult()")
	}
}

// ---- Test 2: variant truncation ---------------------------------------------

// TestDescribeOpTruncatesVariantsToFive verifies that when an op has 7 variants,
// describe_op returns at most 5 in variants[] (the default max_variants) and
// carries variants_total=7 and variants_omitted_count=2.
//
// Spec anchor: test-matrix.md line 98 — "deterministic variants[] truncation form
// controlled by meta_tools.describe_op.max_variants (default 5; ops with 6+
// variants truncate to 5 by default, with variants_total and variants_omitted_count)"
//
// Assertions:
//   - len(variants) == 5 (truncated to default max_variants=5)
//   - variants_total == 7 (total count before truncation)
//   - variants_omitted_count == 2 (7 - 5)
func TestDescribeOpTruncatesVariantsToFive(t *testing.T) {
	op := makeMinimalOp("test.op.many", 7)
	snap := makeMinimalCatalog(op)

	got := callDescribeOp(t, snap, "test.op.many")

	variants, ok := got["variants"].([]any)
	if !ok {
		t.Fatalf("variants is not a JSON array; got %T", got["variants"])
	}
	if len(variants) != 5 {
		t.Errorf("variants length = %d; want 5 (max_variants default; spec test-matrix.md line 98)", len(variants))
	}

	if vt, ok := got["variants_total"].(float64); !ok || int(vt) != 7 {
		t.Errorf("variants_total = %v; want 7 (total before truncation)", got["variants_total"])
	}
	if oc, ok := got["variants_omitted_count"].(float64); !ok || int(oc) != 2 {
		t.Errorf("variants_omitted_count = %v; want 2 (7 - 5)", got["variants_omitted_count"])
	}
}

// ---- Test 3: risk_override round-trip ---------------------------------------

// TestDescribeOpCarriesRiskOverride verifies that when the default variant has
// risk_override=true and a non-empty risk_override_reason, those fields appear
// in the describe_op result.
//
// Spec anchor: spec.md §5.1.2 — "When the resolved variant has risk_override: true,
// gum.describe_op MUST include the provenance in its output"
//
// Assertions:
//   - "risk_override" == true in result
//   - "risk_override_reason" == "Test override reason" in result
func TestDescribeOpCarriesRiskOverride(t *testing.T) {
	op := makeMinimalOp("test.op.override", 1)
	op.Variants[0].RiskOverride = true
	op.Variants[0].RiskOverrideReason = "Test override reason"
	snap := makeMinimalCatalog(op)

	got := callDescribeOp(t, snap, "test.op.override")

	if got["risk_override"] != true {
		t.Errorf("risk_override = %v; want true (spec §5.1.2: must include override provenance)", got["risk_override"])
	}
	if got["risk_override_reason"] != "Test override reason" {
		t.Errorf("risk_override_reason = %v; want %q (spec §5.1.2)", got["risk_override_reason"], "Test override reason")
	}
}

// ---- Test 4: plugin status/reason fields excluded ---------------------------

// TestDescribeOpExcludesPluginStatusFields verifies that even when a catalog.Op
// contains a plugin-backed variant, the describe_op result does NOT include the
// "status" or "reason" fields.
//
// Spec anchor: spec.md DescribeOpResult $comment — "The 'status' and 'reason'
// fields are NOT properties of DescribeOpResult. They appear only on
// gum://op/{id} and gum://variant/{id} resource responses"
// Also: test-matrix.md line 98 — "explicit exclusion of inactive-plugin-only
// status / reason fields"
//
// Assertions:
//   - "status" key is absent from the JSON result
//   - "reason" key is absent from the JSON result
func TestDescribeOpExcludesPluginStatusFields(t *testing.T) {
	op := makeMinimalOp("test.op.plugin", 1)
	// Wire as a plugin-backed variant.
	op.Variants[0].InterfaceKind = catalog.InterfaceKindPluginMCP
	op.Variants[0].BackendKind = catalog.BackendKindMCPPlugin
	snap := makeMinimalCatalog(op)

	got := callDescribeOp(t, snap, "test.op.plugin")

	if _, has := got["status"]; has {
		t.Errorf("status MUST NOT appear in describe_op result (plugin-only field; "+
			"spec DescribeOpResult $comment and test-matrix.md line 98): got %v", got["status"])
	}
	if _, has := got["reason"]; has {
		t.Errorf("reason MUST NOT appear in describe_op result (plugin-only field; "+
			"spec DescribeOpResult $comment and test-matrix.md line 98): got %v", got["reason"])
	}

	// The compact result MUST still carry risk_class and default_variant_id even for
	// plugin-backed ops. These fields prove the compact DescribeOpResult shape is
	// returned (not the raw catalog.Op struct, which would omit risk_class at the top level).
	if got["risk_class"] == nil {
		t.Errorf("risk_class is missing from describe_op result for plugin op; "+
			"the compact DescribeOpResult shape must carry risk_class from the default variant "+
			"(spec §4.1: 'risk class, required scopes, output profile') — "+
			"current impl returns raw catalog.Op which nests risk_class inside variants[]")
	}
	if got["default_variant_id"] == nil {
		t.Errorf("default_variant_id is missing from describe_op result; "+
			"required by DescribeOpResult schema (spec Appendix A)")
	}
}

// ---- Test 5: OP_NOT_FOUND for unknown op_id --------------------------------

// TestDescribeOpUnknownReturnsOpNotFound verifies that describe_op returns a
// structured OP_NOT_FOUND error (IsError=true) carrying the op_id detail when
// an unknown op_id is supplied.
//
// Spec anchor: spec.md §4.1 risk-gate diagram — "op_id in catalog? → NO →
// OP_NOT_FOUND (+ BM25 suggestions)"
// Also: spec.md §4.1 — "{"error_code": "OP_NOT_FOUND", "message": "...",
// "suggestions": [...]}"
//
// Assertions:
//   - result.IsError == true
//   - body contains "OP_NOT_FOUND"
//   - body contains the supplied op_id string "no.such.op"
func TestDescribeOpUnknownReturnsOpNotFound(t *testing.T) {
	snap := makeMinimalCatalog() // empty catalog
	s := NewServerWithCatalog(describeOpDispatcher{}, snap)

	args, _ := json.Marshal(map[string]string{"op_id": "no.such.op"})
	req := &sdkmcp.CallToolRequest{}
	req.Params = &sdkmcp.CallToolParamsRaw{
		Arguments: args,
	}

	res, err := s.handleDescribeOp(context.Background(), req)
	if err != nil {
		t.Fatalf("handleDescribeOp returned Go error: %v", err)
	}
	if res == nil {
		t.Fatal("handleDescribeOp returned nil result")
	}

	// Must be an error result.
	if !res.IsError {
		t.Errorf("IsError = false; want true for unknown op_id (spec §4.1 OP_NOT_FOUND)")
	}

	if len(res.Content) == 0 {
		t.Fatal("no content in error result")
	}
	text, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is not *TextContent")
	}
	body := text.Text

	if !strings.Contains(body, "OP_NOT_FOUND") {
		t.Errorf("error body does not contain OP_NOT_FOUND; got: %s", body)
	}
	if !strings.Contains(body, "no.such.op") {
		t.Errorf("error body does not contain op_id %q; got: %s", "no.such.op", body)
	}

	// Spec §4.1 says the structured OP_NOT_FOUND envelope MUST be JSON with error_code
	// and op_id fields: {"error_code": "OP_NOT_FOUND", "message": "...", "suggestions": [...]}.
	// The current impl returns a plain string "OP_NOT_FOUND: no.such.op", not structured JSON.
	// The green team must return a JSON envelope.
	var envelope map[string]any
	if jsonErr := json.Unmarshal([]byte(body), &envelope); jsonErr != nil {
		t.Errorf("OP_NOT_FOUND error body must be a JSON envelope (spec §4.1: "+
			"{\"error_code\":\"OP_NOT_FOUND\",\"op_id\":\"...\"}); got non-JSON: %s", body)
	} else {
		if envelope["error_code"] == nil {
			t.Errorf("OP_NOT_FOUND envelope missing error_code field; body: %s", body)
		}
		if envelope["op_id"] == nil {
			t.Errorf("OP_NOT_FOUND envelope missing op_id field (spec §4.1: carry op_id detail); body: %s", body)
		}
	}
}

// ---- Test 6: schema_refs are refs not inlined schemas ----------------------

// TestDescribeOpSchemaRefsAreRefsNotInlined verifies that when schema_refs is
// present, its values are string references (e.g. "#/$defs/SomeType" or a
// $ref-style path), not inlined JSON Schema objects.
//
// Spec anchor: spec.md §4.1 — "It does not inline full JSON Schemas; clients
// fetch full active op/variant records through gum://op/<id> and gum://variant/<id>,
// then fetch exact JSON Schema documents through gum://schema/<schema_ref>"
// Also: spec.md DescribeOpResult schema — schema_refs.input and schema_refs.output
// are {"type":"string"}, not {"type":"object"}.
//
// Assertions:
//   - schema_refs is an object (if present and non-null)
//   - schema_refs.input (if present) is a string, not an object
//   - schema_refs.output (if present) is a string, not an object
//   - Neither value starts with "{" (i.e. is not an inlined object serialized as string)
func TestDescribeOpSchemaRefsAreRefsNotInlined(t *testing.T) {
	op := makeMinimalOp("test.op.refs", 1)
	// Attach a binding with request_ref and response_ref.
	op.Variants[0].Binding = &catalog.Binding{
		BindingSchemaVersion: 1,
		AdapterKey:           "gmail",
		OperationKey:         "users.messages.list",
		RequestRef:           "#/$defs/ListMessagesRequest",
		ResponseRef:          "#/$defs/ListMessagesResponse",
	}
	snap := makeMinimalCatalog(op)

	got := callDescribeOp(t, snap, "test.op.refs")

	// schema_refs MUST be present when the default variant has a binding with request/response refs.
	// The current impl returns the raw catalog.Op which does not include a top-level schema_refs key.
	if got["schema_refs"] == nil {
		t.Errorf("schema_refs key is missing; MUST be present when default variant has a binding "+
			"(spec §4.1: 'schema refs' in compact output; DescribeOpResult schema). "+
			"Current impl returns raw catalog.Op — schema_refs must be built from variant.Binding.RequestRef/ResponseRef.")
		return
	}

	schemaRefs, ok := got["schema_refs"].(map[string]any)
	if !ok {
		t.Errorf("schema_refs is present but not an object; got %T: %v", got["schema_refs"], got["schema_refs"])
		return
	}

	// Check each present ref value.
	for _, key := range []string{"input", "output"} {
		val, present := schemaRefs[key]
		if !present {
			continue
		}
		// Must be a string.
		strVal, isStr := val.(string)
		if !isStr {
			t.Errorf("schema_refs.%s must be a string ref, not an inlined schema object; got %T: %v",
				key, val, val)
			continue
		}
		// Must not be an inlined JSON object serialized as a string.
		if strings.HasPrefix(strings.TrimSpace(strVal), "{") {
			t.Errorf("schema_refs.%s looks like an inlined JSON object %q; "+
				"must be a ref string like \"#/$defs/...\" (spec §4.1: no inlined schemas)", key, strVal)
		}
		if strVal == "" {
			t.Errorf("schema_refs.%s is an empty string; expected a non-empty ref", key)
		}
	}
}
