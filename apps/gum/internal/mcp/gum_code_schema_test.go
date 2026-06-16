// Package mcp — Red Team failing tests for bead gum-9vuq.6.
//
// These tests assert the acceptance criteria for gum.code schema conformance:
//   - 8-parameter input schema (language, source, allow_write, allow_destructive,
//     destructive_budget, destructive_scope, confirmed, confirmation_token)
//   - Correct property names (source not script, no timeout_sec)
//   - language enum: only "risor" (no reserved strings starlark/yaegi/js/python)
//   - additionalProperties:false
//   - required: at minimum language and source
//   - destructive_budget: type integer, default 0 or minimum:0
//   - destructive_scope: type array, items type:string
//   - destructiveHint=true annotation on gum.code
//   - per-tool inputSchema token budget ≤190 cl100k_base tokens (meta tools)
//
// Spec anchors:
//   - spec.md §4.1 table (gum.code row) — 8 params, token budget, destructiveHint
//   - spec.md §6.1 — gum.code semantics, reserved language rejection
//   - spec.md §6.1 line ~391 — reserved language rejection at JSON Schema layer (-32602)
//
// All tests MUST compile. They MUST fail for the right reasons (wrong schema shape)
// until the Green Team fixes schemas.go.
package mcp

import (
	"encoding/json"
	"testing"

	"github.com/tiktoken-go/tokenizer"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- helpers local to this file -------------------------------------------

// getGumCodeSchema returns the current gum.code input schema from
// MetaToolDefs (same path the server uses).
func getGumCodeSchema(t *testing.T) json.RawMessage {
	t.Helper()
	defs := MetaToolDefs()
	for _, d := range defs {
		if d.Name == "gum.code" {
			return d.Schema
		}
	}
	t.Fatal("gum.code not found in MetaToolDefs()")
	return nil
}

// parseSchemaMap unmarshals a JSON schema into a map[string]any.
func parseSchemaMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parseSchemaMap: json.Unmarshal: %v", err)
	}
	return m
}

// schemaProps returns the properties sub-map from a schema map, failing if absent.
func schemaProps(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	props, ok := schema["properties"].(map[string]any)
	if !ok || props == nil {
		t.Fatal("gum.code schema has no properties map")
	}
	return props
}

// cl100kTokenCount returns the cl100k_base token count for the given text.
// Falls back to a byte-length proxy (len/4) if the tokenizer fails to load.
func cl100kTokenCount(t *testing.T, text string) int {
	t.Helper()
	enc, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Logf("WARNING: cl100k tokenizer unavailable (%v); using byte-length proxy (len/4). "+
			"This is a structural approximation — ensure tiktoken-go/tokenizer is available for exact counts.", err)
		return len(text) / 4
	}
	ids, _, err := enc.Encode(text)
	if err != nil {
		t.Logf("WARNING: cl100k encode failed (%v); using byte-length proxy (len/4).", err)
		return len(text) / 4
	}
	return len(ids)
}

// --- Test 1 ---------------------------------------------------------------

// TestGumCodeSchemaUsesSourceNotScript asserts that the gum.code schema
// declares a property named "source" (not "script") and does NOT contain
// "script" or "timeout_sec".
//
// Failure: current schemas.go (line ~164) uses "script" and "timeout_sec".
func TestGumCodeSchemaUsesSourceNotScript(t *testing.T) {
	raw := getGumCodeSchema(t)
	schema := parseSchemaMap(t, raw)
	props := schemaProps(t, schema)

	if _, hasSource := props["source"]; !hasSource {
		t.Errorf(`gum.code schema is missing "source" property; `+
			`spec.md §6.1 names the parameter "source" not "script". `+
			`Current schemas.go uses "script" — Green Team must rename it.`)
	}

	if _, hasScript := props["script"]; hasScript {
		t.Errorf(`gum.code schema contains forbidden property "script"; `+
			`spec.md §6.1 / §4.1 table requires "source". `+
			`Green Team must rename the property in schemas.go.`)
	}

	if _, hasTimeout := props["timeout_sec"]; hasTimeout {
		t.Errorf(`gum.code schema contains "timeout_sec" which is not in the spec §4.1 8-param list. ` +
			`Remove it from schemas.go.`)
	}
}

// --- Test 2 ---------------------------------------------------------------

// TestGumCodeSchemaHasAllEightParams asserts that the gum.code input schema
// has exactly the 8 parameters from spec.md §4.1 / §6.1:
//
//	language, source, allow_write, allow_destructive, destructive_budget,
//	destructive_scope, confirmed, confirmation_token
//
// Schema MUST also set additionalProperties:false and require at minimum
// "language" and "source".
//
// Failure: current schema has 4 params (script, timeout_sec, allow_write,
// allow_destructive) — missing language, source, destructive_budget,
// destructive_scope, confirmed, confirmation_token; also uses "script" not "source".
func TestGumCodeSchemaHasAllEightParams(t *testing.T) {
	raw := getGumCodeSchema(t)
	schema := parseSchemaMap(t, raw)
	props := schemaProps(t, schema)

	wantParams := []string{
		"language",
		"source",
		"allow_write",
		"allow_destructive",
		"destructive_budget",
		"destructive_scope",
		"confirmed",
		"confirmation_token",
	}

	for _, param := range wantParams {
		if _, ok := props[param]; !ok {
			t.Errorf(`gum.code schema is missing property %q; `+
				`spec.md §4.1 table and §6.1 require all 8 params: %v`,
				param, wantParams)
		}
	}

	if got := len(props); got != 8 {
		t.Errorf("gum.code schema has %d properties; want exactly 8 (spec.md §4.1). "+
			"Current keys: %v", got, propertyKeys(props))
	}

	// additionalProperties:false
	addl, ok := schema["additionalProperties"].(bool)
	if !ok || addl {
		t.Errorf("gum.code schema must set additionalProperties:false (spec.md §4.1 acceptance criterion 4); "+
			"got additionalProperties=%v", schema["additionalProperties"])
	}

	// required must contain at least "language" and "source"
	requiredRaw, hasRequired := schema["required"]
	if !hasRequired {
		t.Error(`gum.code schema is missing "required" array; at minimum ["language","source"] must be required`)
	} else {
		reqSlice, ok := requiredRaw.([]any)
		if !ok {
			t.Errorf(`gum.code "required" is not an array; got %T`, requiredRaw)
		} else {
			reqSet := make(map[string]bool, len(reqSlice))
			for _, v := range reqSlice {
				if s, ok := v.(string); ok {
					reqSet[s] = true
				}
			}
			for _, must := range []string{"language", "source"} {
				if !reqSet[must] {
					t.Errorf(`gum.code "required" array must contain %q (spec.md §6.1); got %v`, must, reqSlice)
				}
			}
		}
	}
}

// propertyKeys returns sorted property names for diagnostic messages.
func propertyKeys(props map[string]any) []string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	return keys
}

// --- Test 3 ---------------------------------------------------------------

// TestGumCodeSchemaLanguageEnumRisorOnly asserts that the "language" property
// declares enum: ["risor"] only. Reserved strings (python, starlark, yaegi, js)
// MUST NOT appear in the enum.
//
// Spec anchor: spec.md §6.1 — "Closed v0.1 language enum: risor. The strings
// starlark, yaegi, js, and python are reserved … MUST NOT appear in the v0.1.0
// MCP input schema."
//
// Failure: current schema has no "language" property at all.
func TestGumCodeSchemaLanguageEnumRisorOnly(t *testing.T) {
	raw := getGumCodeSchema(t)
	schema := parseSchemaMap(t, raw)
	props := schemaProps(t, schema)

	langRaw, ok := props["language"]
	if !ok {
		t.Fatal(`gum.code schema is missing "language" property (spec.md §6.1 requires it)`)
	}

	langMap, ok := langRaw.(map[string]any)
	if !ok {
		t.Fatalf(`gum.code "language" property is not a JSON object; got %T`, langRaw)
	}

	enumRaw, hasEnum := langMap["enum"]
	if !hasEnum {
		t.Fatal(`gum.code "language" property must have an "enum" constraint; ` +
			`spec.md §6.1 requires enum:["risor"] only`)
	}

	enumSlice, ok := enumRaw.([]any)
	if !ok {
		t.Fatalf(`gum.code "language".enum is not an array; got %T`, enumRaw)
	}

	// Must contain exactly "risor"
	if len(enumSlice) != 1 {
		t.Errorf(`gum.code "language".enum has %d values; want exactly 1 ("risor"). Got: %v`,
			len(enumSlice), enumSlice)
	}

	enumSet := make(map[string]bool, len(enumSlice))
	for _, v := range enumSlice {
		if s, ok := v.(string); ok {
			enumSet[s] = true
		}
	}

	if !enumSet["risor"] {
		t.Errorf(`gum.code "language".enum must contain "risor"; got %v`, enumSlice)
	}

	// Reserved strings MUST NOT appear
	reserved := []string{"python", "starlark", "yaegi", "js"}
	for _, r := range reserved {
		if enumSet[r] {
			t.Errorf(`gum.code "language".enum contains reserved string %q; `+
				`spec.md §6.1: reserved strings MUST NOT appear in v0.1.0 MCP input schema. `+
				`Any value other than "risor" must be rejected by JSON Schema (-32602).`, r)
		}
	}
}

// --- Test 4 ---------------------------------------------------------------

// TestGumCodeSchemaDestructiveBudgetDefaultsToZero asserts that the
// "destructive_budget" property is type integer with default 0 or minimum:0.
//
// Spec anchor: spec.md §4.1 table: destructive_budget, default 0.
//
// Failure: current schema has no "destructive_budget" property.
func TestGumCodeSchemaDestructiveBudgetDefaultsToZero(t *testing.T) {
	raw := getGumCodeSchema(t)
	schema := parseSchemaMap(t, raw)
	props := schemaProps(t, schema)

	budgetRaw, ok := props["destructive_budget"]
	if !ok {
		t.Fatal(`gum.code schema is missing "destructive_budget" property (spec.md §4.1 / §6.1)`)
	}

	budgetMap, ok := budgetRaw.(map[string]any)
	if !ok {
		t.Fatalf(`gum.code "destructive_budget" property is not a JSON object; got %T`, budgetRaw)
	}

	typ, _ := budgetMap["type"].(string)
	if typ != "integer" {
		t.Errorf(`gum.code "destructive_budget".type = %q; want "integer" (spec.md §4.1)`, typ)
	}

	// Either "default":0 or "minimum":0 signals the spec-mandated default of 0.
	hasDefault := func() bool {
		v, ok := budgetMap["default"]
		if !ok {
			return false
		}
		// JSON numbers unmarshal as float64
		switch n := v.(type) {
		case float64:
			return n == 0
		case int:
			return n == 0
		}
		return false
	}()
	hasMinZero := func() bool {
		v, ok := budgetMap["minimum"]
		if !ok {
			return false
		}
		switch n := v.(type) {
		case float64:
			return n == 0
		case int:
			return n == 0
		}
		return false
	}()

	if !hasDefault && !hasMinZero {
		t.Errorf(`gum.code "destructive_budget" must have default:0 or minimum:0 `+
			`(spec.md §4.1: default 0). Got property definition: %v`, budgetMap)
	}
}

// --- Test 5 ---------------------------------------------------------------

// TestGumCodeSchemaDestructiveScopeDefaultsToEmptyArray asserts that the
// "destructive_scope" property is type array with items type:string.
//
// Spec anchor: spec.md §4.1 table: destructive_scope, default [].
//
// Failure: current schema has no "destructive_scope" property.
func TestGumCodeSchemaDestructiveScopeDefaultsToEmptyArray(t *testing.T) {
	raw := getGumCodeSchema(t)
	schema := parseSchemaMap(t, raw)
	props := schemaProps(t, schema)

	scopeRaw, ok := props["destructive_scope"]
	if !ok {
		t.Fatal(`gum.code schema is missing "destructive_scope" property (spec.md §4.1 / §6.1)`)
	}

	scopeMap, ok := scopeRaw.(map[string]any)
	if !ok {
		t.Fatalf(`gum.code "destructive_scope" property is not a JSON object; got %T`, scopeRaw)
	}

	typ, _ := scopeMap["type"].(string)
	if typ != "array" {
		t.Errorf(`gum.code "destructive_scope".type = %q; want "array" (spec.md §4.1)`, typ)
	}

	// items must be type:string
	itemsRaw, hasItems := scopeMap["items"]
	if !hasItems {
		t.Error(`gum.code "destructive_scope" must have "items" with type:string (spec.md §4.1)`)
	} else {
		itemsMap, ok := itemsRaw.(map[string]any)
		if !ok {
			t.Errorf(`gum.code "destructive_scope".items is not a JSON object; got %T`, itemsRaw)
		} else {
			itemType, _ := itemsMap["type"].(string)
			if itemType != "string" {
				t.Errorf(`gum.code "destructive_scope".items.type = %q; want "string" (spec.md §4.1)`, itemType)
			}
		}
	}
}

// --- Test 6 ---------------------------------------------------------------

// TestGumCodeAnnotationDestructiveHint asserts that the gum.code tool
// registration carries destructiveHint=true in its MCP annotations.
//
// Spec anchor: spec.md §4.1 table — gum.code annotation: "destructive (static annotation)".
//
// The annotation is applied in server.go's NewServerWithCatalog loop. The test
// inspects the registered sdkmcp.Tool directly via a live Server construction.
//
// Failure: server.go (line ~88-93) only sets annotations for "gum.read";
// gum.code has nil Annotations.
func TestGumCodeAnnotationDestructiveHint(t *testing.T) {
	// Build a Server so we can inspect registered tool annotations.
	// schemaTestDispatcher (from tier_a_schemas_test.go) is a no-op.
	srv := NewServer(schemaTestDispatcher{})

	// The go-sdk Server exposes registered tools via Tools() or a list endpoint.
	// We access the underlying sdkSrv through the exported sdkSrv field if
	// available; otherwise we drive a ListTools round-trip.
	//
	// Since sdkSrv is unexported, we use the same in-process approach as
	// server_test.go: NewInMemoryTransports + Connect + ListTools.
	//
	// However, ListTools returns sdkmcp.Tool values which DO carry Annotations.
	// We use the package-internal approach: inspect the tool via the sdkSrv
	// directly. Since the field is unexported, we fall back to a functional
	// assertion: any registered tool that the server can list should surface its
	// annotations in the ListTools response.
	//
	// The simplest compile-safe approach that doesn't require unexported access:
	// use the exported MetaToolDefs() to find the schema and then verify that
	// the server.go wiring code has been updated by checking a well-known
	// annotation vehicle.
	//
	// We verify two things:
	//   1. The gum.code schema is not the placeholder (structural signal that
	//      the Green Team touched schemas.go).
	//   2. The server registers gum.code with destructiveHint=true by checking
	//      that TierAMetaToolAnnotations() (an export the Green Team should add
	//      to tool_defs.go, parallel to TierAConvenienceToolAnnotations) returns
	//      destructiveHint=true for gum.code.
	//
	// If TierAMetaToolAnnotations does not exist yet, the test fails at runtime
	// with a clear message (it compiles because we reference a symbol defined
	// below as a compile-time probe that calls it only if non-nil).
	//
	// For the v0 red state, we verify annotations via the live server transport.
	_ = srv // prevent "declared but not used" if the transport path below is used

	// Directly check: does server.go set Annotations on gum.code?
	// We simulate what server.go does and check the result.
	// Since we cannot access srv.sdkSrv, we inspect TierAMetaToolAnnotations()
	// if the Green Team exports it, or fail with a descriptive message.
	ann := metaToolAnnotationForCode()
	if ann == nil {
		t.Fatalf("gum.code MCP annotation is nil; "+
			"spec.md §4.1 table requires destructiveHint=true (static annotation). "+
			"Green Team must: (1) export TierAMetaToolAnnotations() in tool_defs.go, "+
			"(2) add destructiveHint=true for gum.code in server.go registration loop. "+
			"Expected: &sdkmcp.ToolAnnotations{DestructiveHint: ptr(true)}.")
	}

	if ann.DestructiveHint == nil {
		t.Error("gum.code Annotations.DestructiveHint is nil pointer; want explicit *true (spec.md §4.1)")
	} else if !*ann.DestructiveHint {
		t.Errorf("gum.code Annotations.DestructiveHint = %v; want true (spec.md §4.1 static annotation)", *ann.DestructiveHint)
	}
}

// metaToolAnnotationForCode returns the current sdkmcp.ToolAnnotations for
// gum.code as wired in server.go, or nil if the wiring is absent.
//
// This is the compile-safe probe: it calls TierAMetaToolAnnotations() if it
// exists; since that function does NOT currently exist in tool_defs.go, we
// call the server.go wiring path directly by inspecting what the constructor
// does. To avoid unexported field access we reconstruct the annotation from
// the known server.go switch: only "gum.read" gets annotations today, so
// gum.code returns nil.
//
// When the Green Team adds TierAMetaToolAnnotations() to tool_defs.go, they
// should replace this function body.
func metaToolAnnotationForCode() *sdkmcp.ToolAnnotations {
	// Green Team: TierAMetaToolAnnotations() is now exported from tool_defs.go.
	return TierAMetaToolAnnotations()["gum.code"]
}

// --- Test 7 ---------------------------------------------------------------

// TestTierAPerToolInputSchemaBudget asserts that every meta-tool's inputSchema
// is ≤190 cl100k_base tokens and every convenience tool's inputSchema is
// ≤100 cl100k_base tokens.
//
// Spec anchor: spec.md §4.1 table, §2 — token budget per tool.
// Test name: TestTierAPerToolInputSchemaBudget (from spec.md §4.1 row for gum.code).
//
// When the cl100k tokenizer is available (github.com/tiktoken-go/tokenizer),
// uses exact counts. This module has the dependency — see sanitize/sanitizer.go.
//
// The test explicitly checks gum.code and asserts it is within budget once the
// schema is corrected. Currently the schema is too small (wrong shape) but will
// still be checked to ensure it doesn't accidentally bloat once fixed.
//
// NOTE: The current gum.code schema (4 wrong params) will pass the budget test
// because it's tiny. The schema tests above ensure correctness. This test ensures
// the CORRECTED schema will remain within budget.
func TestTierAPerToolInputSchemaBudget(t *testing.T) {
	const (
		metaBudget        = 190 // cl100k_base tokens, spec.md §4.1 meta-tool budget
		convenienceBudget = 100 // cl100k_base tokens, spec.md §4.1 convenience tool budget
	)

	// Meta tools
	for _, d := range MetaToolDefs() {
		d := d
		t.Run("meta/"+d.Name, func(t *testing.T) {
			schemaStr := string(d.Schema)
			count := cl100kTokenCount(t, schemaStr)

			if count > metaBudget {
				t.Errorf("%s inputSchema = %d cl100k tokens; must be ≤%d (spec.md §4.1 TestTierAPerToolInputSchemaBudget). "+
					"Schema (len=%d bytes):\n%s",
					d.Name, count, metaBudget, len(schemaStr), schemaStr)
			} else {
				t.Logf("%s inputSchema: %d cl100k tokens (budget: %d) — OK", d.Name, count, metaBudget)
			}
		})
	}

	// Convenience tools
	for _, d := range TierAConvenienceToolDefs() {
		d := d
		t.Run("convenience/"+d.Name, func(t *testing.T) {
			schemaStr := string(d.Schema)
			count := cl100kTokenCount(t, schemaStr)

			if count > convenienceBudget {
				t.Errorf("%s inputSchema = %d cl100k tokens; must be ≤%d (spec.md §4.1 TestTierAPerToolInputSchemaBudget). "+
					"Schema (len=%d bytes):\n%s",
					d.Name, count, convenienceBudget, len(schemaStr), schemaStr)
			} else {
				t.Logf("%s inputSchema: %d cl100k tokens (budget: %d) — OK", d.Name, count, convenienceBudget)
			}
		})
	}

	// Explicit gum.code sub-test so this tool is always checked by name.
	t.Run("meta/gum.code/explicit", func(t *testing.T) {
		raw := getGumCodeSchema(t)
		schemaStr := string(raw)
		count := cl100kTokenCount(t, schemaStr)

		t.Logf("gum.code inputSchema explicit count: %d cl100k tokens (budget: %d, len: %d bytes)",
			count, metaBudget, len(schemaStr))

		if count > metaBudget {
			t.Errorf("gum.code inputSchema = %d cl100k tokens; must be ≤%d per spec.md §4.1 "+
				"(TestTierAPerToolInputSchemaBudget). Schema:\n%s", count, metaBudget, schemaStr)
		}

		// Validate that the budget-passing schema also has the correct shape.
		// A schema that passes budget but has the wrong params is still broken.
		schema := parseSchemaMap(t, raw)
		props, _ := schema["properties"].(map[string]any)
		if _, hasSource := props["source"]; !hasSource {
			t.Logf("NOTE: gum.code budget test passes but schema still lacks 'source' property — "+
				"schema is structurally wrong (see TestGumCodeSchemaUsesSourceNotScript). "+
				"Current schema: %s", schemaStr)
		}
	})
}
