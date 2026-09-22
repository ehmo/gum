package mcp

// Real proof for test-matrix row 160 (bead gum-6lks), which the testmatrix
// shim of the same name used to delegate to TestTierARegistrationOutputSchemasValid.
// That test only checks root type and $defs presence; the row also claims
// representative results validate, that gum.code carries no ParallelResults
// branch, and that confirmation envelopes are not schema branches.
//
// The ParallelResults clause matters because the spec used to require the
// opposite. gum_parallel is a gum.code script builtin, not a registered MCP
// tool. A snippet that calls it prints the batch envelope, and gum.code's
// shaping wraps printed output in the `data` STRING of a shaped result, so a
// ParallelResults value can never be top-level structuredContent. The
// end-to-end case below runs the real Risor adapter to prove it.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ehmo/gum/internal/adapters"
	"github.com/ehmo/gum/internal/dispatch"
)

// resolveRegistered compiles a registered outputSchema for validation.
func resolveRegistered(t *testing.T, tool string, raw json.RawMessage) *jsonschema.Resolved {
	t.Helper()

	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s: registered outputSchema is not valid JSON: %v", tool, err)
	}

	r, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: registered outputSchema does not resolve: %v", tool, err)
	}
	return r
}

// shapedTools are the meta-tools whose structuredContent comes from
// dispatchAndShape and therefore share shapedResultSchema.
var shapedTools = []string{"gum.read", "gum.write", "gum.destructive", "gum.code"}

// TestTierABranchOutputSchemasCoverEveryShapedBranch validates one
// representative value per §13 shaped branch against each shaped tool's
// registered schema. A branch the schema does not admit is a branch a strict
// client rejects at runtime.
func TestTierABranchOutputSchemasCoverEveryShapedBranch(t *testing.T) {
	branches := map[string]map[string]any{
		"ToonResult": {
			"format":      "toon",
			"toon":        "count: 0\nfields: id,name\n",
			"_expression": minimalExpressionMeta("gmail.search", "gmail.search.v1", "gmail.search"),
		},
		"SingleObjectResult/markdown": {
			"format":      "markdown",
			"data":        "# Title\n\nbody",
			"_expression": minimalExpressionMeta("docs.get", "docs.get.v1", "docs.get"),
		},
		"RawJsonResult": {
			"format":      "json",
			"data":        map[string]any{"id": "abc"},
			"_expression": minimalExpressionMeta("docs.get", "docs.get.v1", "_raw"),
		},
	}

	for _, tool := range shapedTools {
		resolved := resolveRegistered(t, tool, metaToolOutputSchema(tool))
		for name, value := range branches {
			if err := resolved.Validate(value); err != nil {
				t.Errorf("%s: registered schema rejects the %s branch: %v", tool, name, err)
			}
		}
	}
}

// TestTierABranchOutputSchemasKeepObjectRoot pins the go-sdk constraint: a
// schema that selects between branches still needs root type "object", with
// the branch keyword under that root. A bare root-level {"anyOf": [...]} is
// rejected at registration.
func TestTierABranchOutputSchemasKeepObjectRoot(t *testing.T) {
	check := func(tool string, raw json.RawMessage) {
		t.Helper()

		var s map[string]any
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Errorf("%s: outputSchema is not valid JSON: %v", tool, err)
			return
		}
		if typ, _ := s["type"].(string); typ != "object" {
			t.Errorf("%s: outputSchema root type=%q, want \"object\"; go-sdk rejects a branch-only root", tool, typ)
		}

		branchKeywords := 0
		for _, kw := range []string{"anyOf", "oneOf", "allOf"} {
			if _, ok := s[kw]; ok {
				branchKeywords++
			}
		}
		if branchKeywords > 1 {
			t.Errorf("%s: outputSchema mixes %d branch keywords at the root", tool, branchKeywords)
		}
	}

	for _, tool := range metaToolNames {
		check(tool, metaToolOutputSchema(tool))
	}
	for _, tool := range tierAConvenienceToolNamesList {
		check(tool, convenienceToolOutputSchema(tool))
	}
}

// TestShapedBranchesUseAnyOfNotOneOf pins the reason shapedResultSchema uses
// anyOf: a {"format":"json","data":...} value satisfies SingleObjectResult
// and RawJsonResult at once, so oneOf would reject the most common gum.read
// result outright.
func TestShapedBranchesUseAnyOfNotOneOf(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal(shapedResultSchema(), &s); err != nil {
		t.Fatalf("shapedResultSchema is not valid JSON: %v", err)
	}
	if _, ok := s["oneOf"]; ok {
		t.Fatal("shapedResultSchema uses oneOf; a json+data value matches two branches, so oneOf rejects it")
	}
	if _, ok := s["anyOf"]; !ok {
		t.Fatal("shapedResultSchema lost its anyOf branch list")
	}
}

// TestGumCodeSchemaHasNoParallelResultsBranch runs the real Risor adapter on a
// snippet that prints a gum_parallel-shaped batch envelope, then asserts two
// things about the live structuredContent: it validates against the registered
// gum.code schema, and it is a shaped result whose `data` is the printed
// STRING, not the batch envelope itself.
func TestGumCodeSchemaHasNoParallelResultsBranch(t *testing.T) {
	if strings.Contains(string(metaToolOutputSchema("gum.code")), "ParallelResults") {
		t.Fatal("gum.code registered schema references ParallelResults; no gum.code response can produce that value")
	}

	snap := minimalCatalog(gumCodeOpForConfirmationTest())
	disp := dispatch.NewDispatcher(snap, map[string]dispatch.Adapter{
		"code.risor": adapters.NewCodeRunner(),
	})
	srv := NewServerWithCatalog(disp, snap)

	const source = `gum_print({"format": "parallel_results", "batch_id": "b1", "results": []})`
	res, err := srv.handleCode(context.Background(), gumCodeRequest(map[string]any{
		"language": "risor",
		"source":   source,
	}))
	if err != nil {
		t.Fatalf("handleCode: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleCode returned an error result: %#v", res.Content)
	}

	// Round-trip through JSON first: structuredContent holds live Go values
	// (*dispatch.ExpressionMeta among them), and a client validates the
	// serialized form, not the struct.
	encoded, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structuredContent: %v", err)
	}
	var live map[string]any
	if err := json.Unmarshal(encoded, &live); err != nil {
		t.Fatalf("decode structuredContent: %v", err)
	}

	resolved := resolveRegistered(t, "gum.code", metaToolOutputSchema("gum.code"))
	if err := resolved.Validate(live); err != nil {
		t.Fatalf("live gum.code structuredContent fails its own registered schema: %v\ngot %#v", err, live)
	}

	if format, _ := live["format"].(string); format != "json" {
		t.Errorf("live format=%q, want json", format)
	}
	body, ok := live["data"].(string)
	if !ok {
		t.Fatalf("live data is %T, want string; gum.code wraps printed output in a string", live["data"])
	}
	if !strings.Contains(body, `"parallel_results"`) {
		t.Fatalf("printed batch envelope did not land in data: %q", body)
	}
	if _, ok := live["results"]; ok {
		t.Fatal("batch envelope surfaced at the top level; it must stay inside the data string")
	}
}

// TestConfirmationEnvelopeIsNotASchemaBranch pins the other half of row 160: a
// REQUIRES_CONFIRMATION response travels as an isError §7 envelope, so no
// registered branch describes it. If one ever did, the gate below fails and the
// spec claim needs revisiting alongside it.
func TestConfirmationEnvelopeIsNotASchemaBranch(t *testing.T) {
	envelope := map[string]any{
		"error_code":           "REQUIRES_CONFIRMATION",
		"message":              "op drive.files.delete requires confirmed=true with a valid confirmation_token",
		"confirmation_purpose": "gum_confirm_destructive",
		"confirmation_token":   "v1.test",
		"op_id":                "drive.files.delete",
		"risk_class":           "destructive",
		"retryable":            false,
	}

	for _, tool := range shapedTools {
		resolved := resolveRegistered(t, tool, metaToolOutputSchema(tool))
		if err := resolved.Validate(envelope); err == nil {
			t.Errorf("%s: the REQUIRES_CONFIRMATION envelope validates as structuredContent; it is an isError §7 envelope and must not be a schema branch", tool)
		}
	}
}
