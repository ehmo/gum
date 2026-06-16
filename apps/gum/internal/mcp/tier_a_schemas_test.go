// Package mcp — Red Team failing tests for gum-9vuq.12.
//
// These tests assert the acceptance criteria for issue gum-9vuq.12:
// every Tier A tool MUST declare a proper per-tool JSON Schema — no
// placeholder {"type":"object","additionalProperties":true}.
//
// Spec anchors:
//   - spec.md §4.1 — Tier A convenience tools and meta-tools
//   - spec.md §4 table — Tier A surface (9 meta + 18 convenience)
//   - spec.md §2 — per-tool inputSchema token budget (≤100 tokens for
//     convenience, ≤190 for meta)
//   - docs/test-matrix.md rows: TestTierAConvenienceABI, TestTierAMetaToolCount
//
// Required exports that the Green Team MUST add to the mcp package
// (currently missing — tests will fail at compile time until added):
//
//	func TierAConvenienceToolDefs() []ToolDef
//	func MetaToolDefs() []ToolDef
//	type ToolDef struct { Name string; Schema json.RawMessage }
//
// These helpers allow the tests to inspect registered tool schemas without
// spinning up the full MCP server. They are distinct from the Server methods
// MetaToolNames() / ConvenienceToolNames() which return only names.
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// ToolDef is the expected export from the mcp package so tests can inspect
// per-tool schemas without a full server.  The Green Team MUST add this type
// and the two constructor functions below.  Until they exist the file will not
// compile — that is the desired red state.
//
// NOTE: this declaration will collide with the Green Team's export once they
// add it; they should remove this local stub and keep their exported version.
// The Red Team places it here so the file compiles (with compile errors on the
// missing functions) rather than failing with a parse error.

// tierAConvenienceTool is the canonical list of 18 convenience tool names per
// spec.md §4.1 table.  Tests enumerate this list rather than querying the
// server so failures are deterministic even before the roster file exists.
var tierAConvenienceToolNames = []string{
	"gmail_search",
	"gmail_get_message",
	"gmail_send",
	"gmail_create_draft",
	"drive_find",
	"drive_get_file",
	"drive_share",
	"calendar_upcoming",
	"calendar_create_event",
	"calendar_update_event",
	"docs_get",
	"docs_create",
	"sheets_read",
	"sheets_write",
	"slides_get",
	"tasks_list",
	"tasks_create",
	"flights_search",
}

// isPlaceholderSchema returns true when the raw JSON Schema is the forbidden
// placeholder {"type":"object","additionalProperties":true}.
func isPlaceholderSchema(raw json.RawMessage) bool {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	typ, _ := s["type"].(string)
	addl, hasAddl := s["additionalProperties"].(bool)
	props, hasProps := s["properties"]
	// Placeholder: type=object, additionalProperties=true, no real properties.
	return typ == "object" && hasAddl && addl && (props == nil || !hasProps)
}

// schemaHasAdditionalPropertiesFalse returns true when the schema explicitly
// sets additionalProperties to the boolean false.
func schemaHasAdditionalPropertiesFalse(raw json.RawMessage) bool {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	v, ok := s["additionalProperties"].(bool)
	return ok && !v
}

// schemaPropertyCount returns the number of declared properties in the schema.
func schemaPropertyCount(raw json.RawMessage) int {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0
	}
	props, ok := s["properties"].(map[string]any)
	if !ok {
		return 0
	}
	return len(props)
}

// schemaTypeIsObject returns true when the schema declares type=object.
func schemaTypeIsObject(raw json.RawMessage) bool {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	return s["type"] == "object"
}

// --- Test 1 ---------------------------------------------------------------

// TestTierAToolsHaveDeclaredInputSchemas asserts that every Tier A convenience
// tool registered by the Server has a real per-tool JSON Schema — NOT the
// placeholder {"type":"object","additionalProperties":true}.
//
// Spec anchor: spec.md §4.1 ("Each convenience tool … inputSchema is generated
// from the listed required/optional args plus format?").
//
// Failure modes expected:
//   - convenienceToolSchema currently returns the placeholder for every tool
//     → isPlaceholderSchema returns true → test fails per tool
//   - additionalProperties is not set to false → test fails per tool
//   - properties map is empty → test fails per tool
//
// The test also calls TierAConvenienceToolDefs(), which does not yet exist in
// the package → compile error (desired red state).
func TestTierAToolsHaveDeclaredInputSchemas(t *testing.T) {
	// TierAConvenienceToolDefs returns all 18 convenience tools with their
	// registered schemas.  This function does NOT yet exist; the test will
	// fail to compile until the Green Team adds it.
	defs := TierAConvenienceToolDefs()

	if len(defs) != 18 {
		t.Errorf("TierAConvenienceToolDefs returned %d tools; want 18", len(defs))
	}

	for _, d := range defs {
		d := d // capture
		t.Run(d.Name, func(t *testing.T) {
			raw := d.Schema

			if isPlaceholderSchema(raw) {
				t.Errorf("%s: inputSchema is the forbidden placeholder {type:object,additionalProperties:true}; "+
					"each tool MUST declare its own schema (spec.md §4.1)", d.Name)
			}

			if !schemaTypeIsObject(raw) {
				t.Errorf("%s: inputSchema.type must be \"object\"; got something else", d.Name)
			}

			if !schemaHasAdditionalPropertiesFalse(raw) {
				t.Errorf("%s: inputSchema must set additionalProperties:false to reject unknown args "+
					"at the MCP layer (spec.md §4.1 acceptance criterion 4)", d.Name)
			}

			if schemaPropertyCount(raw) < 1 {
				t.Errorf("%s: inputSchema.properties must have at least 1 entry; got 0 "+
					"(acceptance criterion 1+2: list required and optional args)", d.Name)
			}
		})
	}
}

// --- Test 2 ---------------------------------------------------------------

// TestTierAToolsSchemaRejectsUnknown verifies that when an unknown arg
// "foo":"bar" is supplied to gmail_search (representative convenience tool),
// the schema validation fails — i.e. the schema is strict enough to reject
// unknown fields.
//
// Spec anchor: spec.md §4.1 acceptance criterion 4 ("Reject unknown args at
// MCP layer (additionalProperties: false)").
//
// Implementation note: the test validates by inspecting the schema JSON rather
// than by going through the live dispatch pipeline, so it does not require a
// running server.  Once the real schema is declared with
// additionalProperties:false, the test documents the expected shape; a
// secondary assertion simulates JSON Schema evaluation using the schema's
// additionalProperties field.
//
// Failure: gmail_search currently has the placeholder schema
// (additionalProperties:true) so the schema-level check will pass unknown args
// → test fails.
func TestTierAToolsSchemaRejectsUnknown(t *testing.T) {
	defs := TierAConvenienceToolDefs()

	var gmailSearchSchema json.RawMessage
	for _, d := range defs {
		if d.Name == "gmail_search" {
			gmailSearchSchema = d.Schema
			break
		}
	}
	if gmailSearchSchema == nil {
		t.Fatal("gmail_search not found in TierAConvenienceToolDefs(); " +
			"either the tool was not registered or the name is wrong")
	}

	// The schema MUST declare additionalProperties:false.  When it does,
	// an unknown key "foo" would be rejected by any compliant JSON Schema
	// validator.  We assert the schema itself is strict.
	if !schemaHasAdditionalPropertiesFalse(gmailSearchSchema) {
		t.Error("gmail_search inputSchema does not have additionalProperties:false; " +
			"unknown args like {foo:\"bar\"} would be silently accepted — " +
			"violates spec.md §4.1 acceptance criterion 4")
	}

	// Simulate an unknown-arg invocation: build a payload with an unrecognised
	// key and verify the schema's additionalProperties gate would reject it.
	// (We use a structural check because the test must not depend on a
	// third-party JSON Schema library being present in the tree.)
	unknownPayload := json.RawMessage(`{"foo": "bar"}`)
	var payloadMap map[string]any
	if err := json.Unmarshal(unknownPayload, &payloadMap); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(gmailSearchSchema, &schema); err != nil {
		t.Fatalf("json.Unmarshal schema: %v", err)
	}

	props, _ := schema["properties"].(map[string]any)
	addlFalse := func() bool {
		v, ok := schema["additionalProperties"].(bool)
		return ok && !v
	}()

	if addlFalse {
		for key := range payloadMap {
			if _, defined := props[key]; !defined {
				// Good: the schema would reject this key.
				t.Logf("unknown arg %q correctly rejected by additionalProperties:false", key)
			}
		}
	} else {
		t.Error("gmail_search inputSchema permits unknown args (additionalProperties is not false); " +
			"passing {foo:\"bar\"} would not be rejected at the MCP layer")
	}
}

// --- Test 3 ---------------------------------------------------------------

// TestTierAToolsSchemaRejectsTypeError verifies that gmail_search's maxResults
// arg is declared as type integer so that passing the string "ten" would fail
// JSON Schema validation.
//
// Spec anchor: spec.md §4.1 table row for gmail.search: optional arg maxResults
// (integer per the underlying API).
//
// Failure: current placeholder schema has no properties at all, so maxResults
// type constraint is absent → test fails.
func TestTierAToolsSchemaRejectsTypeError(t *testing.T) {
	defs := TierAConvenienceToolDefs()

	var gmailSearchSchema json.RawMessage
	for _, d := range defs {
		if d.Name == "gmail_search" {
			gmailSearchSchema = d.Schema
			break
		}
	}
	if gmailSearchSchema == nil {
		t.Fatal("gmail_search not found in TierAConvenienceToolDefs()")
	}

	var schema map[string]any
	if err := json.Unmarshal(gmailSearchSchema, &schema); err != nil {
		t.Fatalf("json.Unmarshal schema: %v", err)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		t.Fatal("gmail_search inputSchema has no properties; " +
			"maxResults type constraint is absent — passing string \"ten\" " +
			"would not be rejected at the MCP layer")
	}

	maxResultsDef, exists := props["maxResults"]
	if !exists {
		t.Error("gmail_search inputSchema is missing the maxResults property; " +
			"the spec §4.1 table lists it as an optional arg with integer type")
		return
	}

	maxResultsMap, ok := maxResultsDef.(map[string]any)
	if !ok {
		t.Error("gmail_search maxResults property is not a JSON object")
		return
	}

	typ, _ := maxResultsMap["type"].(string)
	if typ != "integer" {
		t.Errorf("gmail_search maxResults.type = %q; want \"integer\" — "+
			"passing the string \"ten\" must fail JSON Schema validation "+
			"(spec.md §4.1 acceptance criterion 3)", typ)
	}
}

// --- Test 4 ---------------------------------------------------------------

// TestNineMetaToolsAlsoHaveSchemas verifies that the 9 meta-tools also declare
// real schemas: type=object, additionalProperties:false, at least 1 property
// (except gum.cache_stats and gum.gain which may have 0 required args, but
// still must not use the placeholder).
//
// Spec anchor: spec.md §4.1 normative meta-tool registry; test-matrix.md
// TestTierAMetaToolCount.
//
// Failure: most meta-tool schemas currently lack additionalProperties:false.
// The test calls MetaToolDefs() which does not yet exist → compile error.
func TestNineMetaToolsAlsoHaveSchemas(t *testing.T) {
	defs := MetaToolDefs()

	if len(defs) != 9 {
		t.Errorf("MetaToolDefs returned %d tools; want 9", len(defs))
	}

	// Tools that legitimately have zero declared properties but MUST still
	// not be the placeholder.
	zeroArgAllowed := map[string]bool{
		"gum.cache_stats": true,
		"gum.gain":        true,
	}

	for _, d := range defs {
		d := d
		t.Run(d.Name, func(t *testing.T) {
			raw := d.Schema

			if isPlaceholderSchema(raw) {
				t.Errorf("%s: meta-tool inputSchema is the forbidden placeholder; "+
					"must declare a proper schema (spec.md §4.1)", d.Name)
			}

			if !schemaTypeIsObject(raw) {
				t.Errorf("%s: inputSchema.type must be \"object\"", d.Name)
			}

			if !schemaHasAdditionalPropertiesFalse(raw) {
				t.Errorf("%s: inputSchema must set additionalProperties:false "+
					"(spec.md §4.1 acceptance criterion 4)", d.Name)
			}

			if !zeroArgAllowed[d.Name] && schemaPropertyCount(raw) < 1 {
				t.Errorf("%s: inputSchema.properties must have at least 1 entry", d.Name)
			}
		})
	}
}

// TestTierAInputSchemasAvoidTopLevelCombinators guards MCP client
// compatibility. Some clients reject tool input_schema documents that use
// top-level oneOf/allOf/anyOf/not. gum's argument schemas stay plain objects;
// handler code enforces any cross-field rules at runtime.
func TestTierAInputSchemasAvoidTopLevelCombinators(t *testing.T) {
	for _, d := range append(MetaToolDefs(), TierAConvenienceToolDefs()...) {
		var schema map[string]any
		if err := json.Unmarshal(d.Schema, &schema); err != nil {
			t.Fatalf("%s: schema is not valid JSON: %v", d.Name, err)
		}
		for _, key := range []string{"oneOf", "allOf", "anyOf", "not"} {
			if _, ok := schema[key]; ok {
				t.Errorf("%s: inputSchema uses top-level %s; keep tool schemas as plain objects for MCP client compatibility", d.Name, key)
			}
		}
		if got := schema["type"]; got != "object" {
			t.Errorf("%s: inputSchema.type=%v; want object", d.Name, got)
		}
	}
}

// --- Test 5 ---------------------------------------------------------------

// TestTierARosterFileExists verifies that docs/tier-a-roster.v1.json exists,
// parses as JSON, and lists exactly 18 convenience tools.
//
// Spec anchor: spec.md §4.1 ("docs/tier-a-roster.v1.json is the
// machine-readable source of truth for the Tier A roster count and names").
//
// The file currently lives at internal/embedded/data/tier-a-roster.v1.json
// (the embedded copy) but the spec requires it to also exist at
// docs/tier-a-roster.v1.json as the human-readable contract source.
//
// Failure mode: docs/tier-a-roster.v1.json does not exist at the docs/ path
// (only the embedded copy exists) → os.ReadFile fails → test fails.
// Additionally, the current roster uses dot-notation names (gmail.search) not
// snake_case (gmail_search) → count assertion may pass but name-shape tests
// will catch the mismatch.
func TestTierARosterFileExists(t *testing.T) {
	// Path from the module root (apps/gum); tests run with wd = module root.
	const rosterPath = "../../docs/tier-a-roster.v1.json"

	data, err := os.ReadFile(rosterPath)
	if err != nil {
		t.Fatalf("docs/tier-a-roster.v1.json not found at %s: %v\n"+
			"Spec §4.1 requires this file as the machine-readable roster source of truth. "+
			"(gum-vzx deliverable; this test fails until that issue is resolved.)", rosterPath, err)
	}

	var roster struct {
		ConvenienceTools []string `json:"convenience_tools"`
		MetaTools        []string `json:"meta_tools"`
	}
	if err := json.Unmarshal(data, &roster); err != nil {
		t.Fatalf("docs/tier-a-roster.v1.json: parse error: %v", err)
	}

	if len(roster.ConvenienceTools) != 18 {
		t.Errorf("docs/tier-a-roster.v1.json lists %d convenience_tools; want 18 "+
			"(spec.md §4.1: hard cap of 18 convenience tools in v0.1)", len(roster.ConvenienceTools))
	}

	if len(roster.MetaTools) != 9 {
		t.Errorf("docs/tier-a-roster.v1.json lists %d meta_tools; want 9 "+
			"(spec.md §4.1 normative meta-tool registry)", len(roster.MetaTools))
	}
}

// --- Test 6 ---------------------------------------------------------------

// TestTierAToolNameSnakeCase verifies that all 18 convenience tool names
// registered by the server follow the ^[a-z][a-z0-9_]+$ snake_case pattern
// (e.g. gmail_search, not gmail.search).
//
// Spec anchor: spec.md §4.1 ("convenience tool MCP names follow
// <service>_<verb> snake_case").
//
// Failure: the current implementation registers tools with dot-notation names
// (gmail.search, drive.find, etc.) which contain dots — violating the
// snake_case pattern.  The regex ^[a-z][a-z0-9_]+$ does not match dots.
func TestTierAToolNameSnakeCase(t *testing.T) {
	snakeCase := regexp.MustCompile(`^[a-z][a-z0-9_]+$`)

	srv := NewServer(schemaTestDispatcher{})
	names := srv.ConvenienceToolNames()

	if len(names) != 18 {
		t.Errorf("ConvenienceToolNames returned %d names; want 18", len(names))
	}

	for _, name := range names {
		if !snakeCase.MatchString(name) {
			t.Errorf("convenience tool name %q does not match snake_case pattern "+
				"^[a-z][a-z0-9_]+$ (spec.md §4.1: names must be <service>_<verb> "+
				"snake_case, e.g. gmail_search not gmail.search)", name)
		}
	}

	// Also verify the canonical list matches exactly — no missing, no extra.
	registeredSet := make(map[string]bool, len(names))
	for _, n := range names {
		registeredSet[n] = true
	}
	for _, expected := range tierAConvenienceToolNames {
		if !registeredSet[expected] {
			t.Errorf("expected convenience tool %q is not registered "+
				"(spec.md §4.1 table)", expected)
		}
	}
}

// schemaTestDispatcher is a minimal no-op dispatcher for structural tests
// that do not exercise the handler path.
type schemaTestDispatcher struct{}

func (schemaTestDispatcher) Dispatch(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	panic("schemaTestDispatcher.Dispatch must not be called in schema tests")
}
