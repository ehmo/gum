// Package mcp — Red Team failing tests for bead gum-9vuq.4.
//
// These tests assert the acceptance criteria for gum.write schema conformance:
//   - 9-parameter input schema (op_id, args, variant_id, fields, page_size,
//     page_token, format, confirmed, confirmation_token)
//   - additionalProperties:false
//   - required: at minimum op_id
//   - format enum: ["toon","csv","json","markdown"] (spec §13 / §4.1)
//   - confirmed: type boolean (optional)
//   - confirmation_token: type string (optional)
//   - high_stakes_write confirmation gate annotation path
//   - MCP annotation: readOnlyHint=false, destructiveHint=false (spec §13 table)
//
// Spec anchors:
//   - spec.md §4.1 table (gum.write row) — 9 params, token budget, annotations
//   - spec.md §4.1 line ~294 — gum.write semantics, MCP annotation table
//   - spec.md §6.1 — high_stakes_write confirmation gate
//   - spec.md §13 annotation wire-form contract
//
// All tests MUST compile. They MUST fail for the right reasons (wrong schema
// shape / missing annotation) until the Green Team fixes schemas.go and
// tool_defs.go.
package mcp

import (
	"encoding/json"
	"testing"
)

// --- helpers local to this file -------------------------------------------

// getGumWriteSchema returns the current gum.write input schema from
// MetaToolDefs (same path the server uses).
func getGumWriteSchema(t *testing.T) json.RawMessage {
	t.Helper()
	defs := MetaToolDefs()
	for _, d := range defs {
		if d.Name == "gum.write" {
			return d.Schema
		}
	}
	t.Fatal("gum.write not found in MetaToolDefs()")
	return nil
}

// parseGumWriteSchemaMap unmarshals the gum.write schema into map[string]any.
func parseGumWriteSchemaMap(t *testing.T) map[string]any {
	t.Helper()
	raw := getGumWriteSchema(t)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parseGumWriteSchemaMap: json.Unmarshal: %v", err)
	}
	return m
}

// gumWriteProps returns the properties sub-map, failing if absent.
func gumWriteProps(t *testing.T) map[string]any {
	t.Helper()
	schema := parseGumWriteSchemaMap(t)
	props, ok := schema["properties"].(map[string]any)
	if !ok || props == nil {
		t.Fatal("gum.write schema has no properties map")
	}
	return props
}

// --- Test 1 ---------------------------------------------------------------

// TestGumWriteSchemaHasAllNineParams asserts that the gum.write input schema
// has exactly the 9 parameters from spec.md §4.1 line ~283:
//
//	op_id, args, variant_id, fields, page_size, page_token, format,
//	confirmed, confirmation_token
//
// Schema MUST also set additionalProperties:false and require at minimum op_id.
//
// Failure: current schemas.go gum.write case (line ~135) has only 4 params
// (op_id, args, allow_write, format) — missing variant_id, fields, page_size,
// page_token, confirmed, confirmation_token; also has a spurious allow_write
// that is not in the spec §4.1 9-param list. format enum is also wrong.
func TestGumWriteSchemaHasAllNineParams(t *testing.T) {
	raw := getGumWriteSchema(t)
	schema := parseSchemaMap(t, raw) // from gum_code_schema_test.go
	props := schemaProps(t, schema)  // from gum_code_schema_test.go

	// Spec §4.1 line ~283: the canonical 9 params for gum.write.
	wantParams := []string{
		"op_id",
		"args",
		"variant_id",
		"fields",
		"page_size",
		"page_token",
		"format",
		"confirmed",
		"confirmation_token",
	}

	for _, param := range wantParams {
		if _, ok := props[param]; !ok {
			t.Errorf(`gum.write schema is missing property %q; `+
				`spec.md §4.1 table (line ~283) requires all 9 params: %v`,
				param, wantParams)
		}
	}

	// Exact count: 9. Any extra params (e.g. spurious allow_write) must be removed.
	if got := len(props); got != 9 {
		t.Errorf("gum.write schema has %d properties; want exactly 9 (spec.md §4.1). "+
			"Current keys: %v", got, propertyKeys(props)) // propertyKeys from gum_code_schema_test.go
	}

	// additionalProperties:false (spec §4.1 acceptance criterion 4)
	addl, ok := schema["additionalProperties"].(bool)
	if !ok || addl {
		t.Errorf("gum.write schema must set additionalProperties:false (spec.md §4.1 criterion 4); "+
			"got additionalProperties=%v", schema["additionalProperties"])
	}

	// required must contain at least "op_id"
	requiredRaw, hasRequired := schema["required"]
	if !hasRequired {
		t.Error(`gum.write schema is missing "required" array; at minimum ["op_id"] must be required`)
	} else {
		reqSlice, ok := requiredRaw.([]any)
		if !ok {
			t.Errorf(`gum.write "required" is not an array; got %T`, requiredRaw)
		} else {
			reqSet := make(map[string]bool, len(reqSlice))
			for _, v := range reqSlice {
				if s, ok := v.(string); ok {
					reqSet[s] = true
				}
			}
			if !reqSet["op_id"] {
				t.Errorf(`gum.write "required" array must contain "op_id" (spec.md §4.1); got %v`, reqSlice)
			}
			// confirmed and confirmation_token MUST NOT be in required — they are optional.
			for _, shouldBeOptional := range []string{"confirmed", "confirmation_token"} {
				if reqSet[shouldBeOptional] {
					t.Errorf(`gum.write "required" must NOT contain %q — it is optional `+
						`(only needed for high_stakes_write flows per spec.md §6.1); got required=%v`,
						shouldBeOptional, reqSlice)
				}
			}
		}
	}
}

// --- Test 2 ---------------------------------------------------------------

// TestGumWriteSchemaFormatEnum asserts that the "format" property declares the
// correct closed enum per spec.md §13 / §4.1 line ~3205:
//
//	["toon", "csv", "json", "markdown"]
//
// Failure: current schemas.go gum.write uses enum:["toon","json","raw"] which
// omits "csv" and "markdown" and includes spurious "raw".
func TestGumWriteSchemaFormatEnum(t *testing.T) {
	props := gumWriteProps(t)

	formatRaw, ok := props["format"]
	if !ok {
		t.Fatal(`gum.write schema is missing "format" property (spec.md §4.1 / §13)`)
	}

	formatMap, ok := formatRaw.(map[string]any)
	if !ok {
		t.Fatalf(`gum.write "format" property is not a JSON object; got %T`, formatRaw)
	}

	enumRaw, hasEnum := formatMap["enum"]
	if !hasEnum {
		t.Fatal(`gum.write "format" property must have an "enum" constraint; ` +
			`spec.md §13 / §4.1 requires enum:["toon","csv","json","markdown"]`)
	}

	enumSlice, ok := enumRaw.([]any)
	if !ok {
		t.Fatalf(`gum.write "format".enum is not an array; got %T`, enumRaw)
	}

	// Must have exactly 4 values.
	if len(enumSlice) != 4 {
		t.Errorf(`gum.write "format".enum has %d values; want exactly 4. Got: %v`,
			len(enumSlice), enumSlice)
	}

	enumSet := make(map[string]bool, len(enumSlice))
	for _, v := range enumSlice {
		if s, ok := v.(string); ok {
			enumSet[s] = true
		}
	}

	// Required values per spec.md §13 / §3205.
	wantEnum := []string{"toon", "csv", "json", "markdown"}
	for _, want := range wantEnum {
		if !enumSet[want] {
			t.Errorf(`gum.write "format".enum is missing %q; `+
				`spec.md §13 requires closed enum ["toon","csv","json","markdown"]. Got: %v`,
				want, enumSlice)
		}
	}

	// "raw" MUST NOT appear — it is not in the spec-mandated closed enum for gum.write.
	if enumSet["raw"] {
		t.Errorf(`gum.write "format".enum contains "raw" which is not in the spec-mandated ` +
			`closed enum (spec.md §13: ["toon","csv","json","markdown"]). ` +
			`Current schemas.go uses ["toon","json","raw"] — Green Team must fix it.`)
	}
}

// --- Test 3 ---------------------------------------------------------------

// TestGumWriteSchemaConfirmationFields asserts that:
//   - "confirmed" is type:boolean (default:false acceptable)
//   - "confirmation_token" is type:string
//   - Both are optional (NOT in "required")
//
// Spec anchor: spec.md §6.1 — confirmation gate for high_stakes_write variants;
// spec.md §4.1 line ~283 — both params are marked optional (trailing ?).
//
// Failure: current schemas.go gum.write case has neither "confirmed" nor
// "confirmation_token" properties.
func TestGumWriteSchemaConfirmationFields(t *testing.T) {
	raw := getGumWriteSchema(t)
	schema := parseSchemaMap(t, raw)
	props := schemaProps(t, schema)

	// --- confirmed ---
	confirmedRaw, ok := props["confirmed"]
	if !ok {
		t.Fatal(`gum.write schema is missing "confirmed" property; ` +
			`spec.md §6.1 requires it for the high_stakes_write confirmation gate; ` +
			`spec.md §4.1 table lists confirmed? as the 8th param.`)
	}

	confirmedMap, ok := confirmedRaw.(map[string]any)
	if !ok {
		t.Fatalf(`gum.write "confirmed" property is not a JSON object; got %T`, confirmedRaw)
	}

	typ, _ := confirmedMap["type"].(string)
	if typ != "boolean" {
		t.Errorf(`gum.write "confirmed".type = %q; want "boolean" (spec.md §6.1)`, typ)
	}

	// --- confirmation_token ---
	tokenRaw, ok := props["confirmation_token"]
	if !ok {
		t.Fatal(`gum.write schema is missing "confirmation_token" property; ` +
			`spec.md §6.1 requires it for the high_stakes_write confirmation gate; ` +
			`spec.md §4.1 table lists confirmation_token? as the 9th param.`)
	}

	tokenMap, ok := tokenRaw.(map[string]any)
	if !ok {
		t.Fatalf(`gum.write "confirmation_token" property is not a JSON object; got %T`, tokenRaw)
	}

	tokenType, _ := tokenMap["type"].(string)
	if tokenType != "string" {
		t.Errorf(`gum.write "confirmation_token".type = %q; want "string" (spec.md §6.1)`, tokenType)
	}

	// Both fields are optional — MUST NOT appear in "required".
	requiredRaw, hasRequired := schema["required"]
	if hasRequired {
		reqSlice, ok := requiredRaw.([]any)
		if ok {
			reqSet := make(map[string]bool, len(reqSlice))
			for _, v := range reqSlice {
				if s, ok := v.(string); ok {
					reqSet[s] = true
				}
			}
			for _, shouldBeOptional := range []string{"confirmed", "confirmation_token"} {
				if reqSet[shouldBeOptional] {
					t.Errorf(`gum.write "required" must NOT contain %q; `+
						`spec.md §6.1 / §4.1 table: both confirmation fields are optional `+
						`(triggered by the server for high_stakes_write variants, `+
						`not required on every call). Got required=%v`,
						shouldBeOptional, reqSlice)
				}
			}
		}
	}
}

// --- Test 4 ---------------------------------------------------------------

// TestGumWriteAnnotationReadOnlyHintFalse asserts that TierAMetaToolAnnotations()
// exposes an entry for "gum.write" with:
//   - Non-nil annotation pointer
//   - ReadOnlyHint == false (the plain bool field is the zero value = false)
//   - DestructiveHint is a non-nil *bool set to false (explicit false pointer,
//     not absent — spec.md §13 wire-form: destructiveHint MUST be present for
//     every Tier A tool)
//
// Spec anchor: spec.md §13 annotation table — gum.write: readOnlyHint=false,
// destructiveHint=false. Wire form: readOnlyHint may be absent (omitempty,
// false is zero value), but destructiveHint MUST be an explicit *bool pointer.
//
// Failure (expected): TierAMetaToolAnnotations() only exposes "gum.read" and
// "gum.code" today. "gum.write" is absent → the returned annotation is nil.
// The test fails with a clear message directing the Green Team to add the entry.
func TestGumWriteAnnotationReadOnlyHintFalse(t *testing.T) {
	annotations := TierAMetaToolAnnotations()

	ann, ok := annotations["gum.write"]
	if !ok || ann == nil {
		t.Fatalf(`TierAMetaToolAnnotations() does not contain an entry for "gum.write"; `+
			`spec.md §13 annotation table requires readOnlyHint=false, destructiveHint=false. `+
			`Green Team must add "gum.write": {ReadOnlyHint: false, DestructiveHint: boolPtr(false)} `+
			`to TierAMetaToolAnnotations() in tool_defs.go. `+
			`Current map keys: %v`, mapKeys(annotations))
	}

	// ReadOnlyHint is a plain bool (not pointer) in go-sdk v1.6.0.
	// gum.write must NOT be a read-only tool — ReadOnlyHint must be false.
	if ann.ReadOnlyHint {
		t.Errorf(`gum.write Annotations.ReadOnlyHint = true; want false. `+
			`spec.md §13 table: gum.write readOnlyHint=false (it mutates state). `)
	}

	// DestructiveHint must be an explicit *bool(false), not nil.
	// spec.md §13 wire-form: "destructiveHint MUST be present with true or false
	// for every Tier A tool" — so nil is non-compliant.
	if ann.DestructiveHint == nil {
		t.Errorf(`gum.write Annotations.DestructiveHint is nil; `+
			`spec.md §13 wire-form requires destructiveHint MUST be present as an explicit `+
			`*bool for every Tier A tool. Green Team: set DestructiveHint: boolPtr(false).`)
	} else if *ann.DestructiveHint {
		t.Errorf(`gum.write Annotations.DestructiveHint = true; want *false. `+
			`spec.md §13 table: gum.write destructiveHint=false — write ops are NOT destructive `+
			`in the MCP annotation model (high-stakes confirmation is a GUM policy field, `+
			`not an MCP destructive annotation per spec §13 note on gum.write row).`)
	}
}

// mapKeys returns the keys of a map for diagnostic messages.
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
