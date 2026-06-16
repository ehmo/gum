// Package mcp — Red Team failing tests for bead gum-9vuq.5.
//
// These tests assert the acceptance criteria for gum.destructive schema
// conformance:
//   - 9-parameter input schema (op_id, args, variant_id, fields, page_size,
//     page_token, format, confirmed, confirmation_token) — spec §4.1 line ~284
//   - additionalProperties:false
//   - required: exactly ["op_id","args"]
//   - format enum: ["toon","csv","json","markdown"] — spec §13
//   - confirmed: type boolean (optional — NOT in required)
//   - confirmation_token: type string (optional — NOT in required)
//   - MCP annotation: readOnlyHint=false, destructiveHint=true — spec §13
//
// Spec anchors:
//   - spec.md §4.1 line ~284 — gum.destructive 9-param row
//   - spec.md §4.1 line ~295 — gum.destructive semantics, MCP annotation
//   - spec.md §6.1 — confirmation gate (requires_confirmation envelope)
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

// getGumDestructiveSchema returns the current gum.destructive input schema
// from MetaToolDefs (same path the server uses at registration time).
func getGumDestructiveSchema(t *testing.T) json.RawMessage {
	t.Helper()
	defs := MetaToolDefs()
	for _, d := range defs {
		if d.Name == "gum.destructive" {
			return d.Schema
		}
	}
	t.Fatal("gum.destructive not found in MetaToolDefs()")
	return nil
}

// parseDestructiveSchema decodes the gum.destructive schema into a generic map
// for property-level assertions.
func parseDestructiveSchema(t *testing.T) map[string]any {
	t.Helper()
	raw := getGumDestructiveSchema(t)
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("failed to unmarshal gum.destructive schema: %v", err)
	}
	return schema
}

// getDestructiveProperties extracts the "properties" map from the schema.
func getDestructiveProperties(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	raw, ok := schema["properties"]
	if !ok {
		t.Fatal("gum.destructive schema missing 'properties' key")
	}
	props, ok := raw.(map[string]any)
	if !ok {
		t.Fatal("gum.destructive schema 'properties' is not an object")
	}
	return props
}

// getDestructiveRequired extracts the "required" array from the schema as a
// []string slice.
func getDestructiveRequired(t *testing.T, schema map[string]any) []string {
	t.Helper()
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatal("gum.destructive schema 'required' is not an array")
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("gum.destructive 'required' contains non-string value: %T", v)
		}
		result = append(result, s)
	}
	return result
}

// --- tests ----------------------------------------------------------------

// TestGumDestructiveSchemaHasAllNineParams asserts that gum.destructive exposes
// exactly the 9-parameter shape mandated by spec §4.1 line ~284:
//
//	op_id, args, variant_id, fields, page_size, page_token, format,
//	confirmed, confirmation_token
func TestGumDestructiveSchemaHasAllNineParams(t *testing.T) {
	schema := parseDestructiveSchema(t)
	props := getDestructiveProperties(t, schema)

	// spec §4.1 line ~284 — the exact 9-param list for gum.destructive.
	want := []string{
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

	// Check for missing properties.
	for _, name := range want {
		if _, exists := props[name]; !exists {
			t.Errorf("gum.destructive schema missing property %q (spec §4.1 line ~284)", name)
		}
	}

	// Check for extra properties not in the spec list.
	wantSet := make(map[string]bool, len(want))
	for _, n := range want {
		wantSet[n] = true
	}
	for name := range props {
		if !wantSet[name] {
			t.Errorf("gum.destructive schema has unexpected property %q not in spec §4.1 9-param list", name)
		}
	}

	// Count check as a summary assertion.
	if got := len(props); got != 9 {
		t.Errorf("gum.destructive schema has %d properties, want exactly 9 (spec §4.1 line ~284)", got)
	}
}

// TestGumDestructiveSchemaFormatEnum asserts that the "format" property is a
// closed enum of exactly ["toon","csv","json","markdown"] per spec §13 /
// §4.1 line ~295. Specifically rejects "raw" if present.
func TestGumDestructiveSchemaFormatEnum(t *testing.T) {
	schema := parseDestructiveSchema(t)
	props := getDestructiveProperties(t, schema)

	formatProp, ok := props["format"]
	if !ok {
		t.Fatal("gum.destructive schema missing 'format' property (spec §4.1 line ~284)")
	}
	formatObj, ok := formatProp.(map[string]any)
	if !ok {
		t.Fatal("gum.destructive schema 'format' is not an object")
	}

	rawEnum, ok := formatObj["enum"]
	if !ok {
		t.Fatal("gum.destructive schema 'format' missing 'enum' key (spec §13 requires closed enum)")
	}
	enumArr, ok := rawEnum.([]any)
	if !ok {
		t.Fatal("gum.destructive schema 'format.enum' is not an array")
	}

	gotEnum := make([]string, 0, len(enumArr))
	for _, v := range enumArr {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("gum.destructive 'format.enum' contains non-string: %T", v)
		}
		gotEnum = append(gotEnum, s)
	}

	// spec §13 / §4.1 line ~295 — closed enum for all write-class meta-tools.
	wantEnum := []string{"toon", "csv", "json", "markdown"}
	wantSet := map[string]bool{"toon": true, "csv": true, "json": true, "markdown": true}
	gotSet := make(map[string]bool, len(gotEnum))
	for _, v := range gotEnum {
		gotSet[v] = true
	}

	for _, v := range wantEnum {
		if !gotSet[v] {
			t.Errorf("gum.destructive format enum missing %q (spec §13)", v)
		}
	}
	for _, v := range gotEnum {
		if !wantSet[v] {
			t.Errorf("gum.destructive format enum contains forbidden value %q (spec §13 closed enum)", v)
		}
	}
	if len(gotEnum) != len(wantEnum) {
		t.Errorf("gum.destructive format enum has %d values %v, want exactly %v (spec §13)", len(gotEnum), gotEnum, wantEnum)
	}
}

// TestGumDestructiveSchemaConfirmationFieldsOptional asserts that "confirmed"
// (boolean) and "confirmation_token" (string) are present as properties but
// are NOT listed in the "required" array. This is the critical contract: a
// caller must be able to invoke gum.destructive WITHOUT confirmed=true so the
// dispatcher can return the requires_confirmation envelope (spec §6.1 / §4.1
// line ~295).
func TestGumDestructiveSchemaConfirmationFieldsOptional(t *testing.T) {
	schema := parseDestructiveSchema(t)
	props := getDestructiveProperties(t, schema)
	required := getDestructiveRequired(t, schema)

	requiredSet := make(map[string]bool, len(required))
	for _, r := range required {
		requiredSet[r] = true
	}

	// confirmed: must exist as boolean property and NOT be required.
	if confirmedProp, ok := props["confirmed"]; !ok {
		t.Error("gum.destructive schema missing 'confirmed' property (spec §4.1 line ~284)")
	} else {
		propObj, ok := confirmedProp.(map[string]any)
		if !ok {
			t.Error("gum.destructive 'confirmed' property is not an object")
		} else if typ, _ := propObj["type"].(string); typ != "boolean" {
			t.Errorf("gum.destructive 'confirmed' has type %q, want \"boolean\" (spec §4.1)", typ)
		}
		if requiredSet["confirmed"] {
			t.Error("gum.destructive 'confirmed' must NOT be in 'required' — caller must be able to omit it to trigger requires_confirmation envelope (spec §6.1)")
		}
	}

	// confirmation_token: must exist as string property and NOT be required.
	if tokenProp, ok := props["confirmation_token"]; !ok {
		t.Error("gum.destructive schema missing 'confirmation_token' property (spec §4.1 line ~284)")
	} else {
		propObj, ok := tokenProp.(map[string]any)
		if !ok {
			t.Error("gum.destructive 'confirmation_token' property is not an object")
		} else if typ, _ := propObj["type"].(string); typ != "string" {
			t.Errorf("gum.destructive 'confirmation_token' has type %q, want \"string\" (spec §4.1)", typ)
		}
		if requiredSet["confirmation_token"] {
			t.Error("gum.destructive 'confirmation_token' must NOT be in 'required' — token is absent on first call (spec §6.1)")
		}
	}
}

// TestGumDestructiveSchemaRequiredOnlyOpIdAndArgs asserts that the "required"
// array is exactly ["op_id","args"] — no more, no less (spec §4.1 line ~284).
func TestGumDestructiveSchemaRequiredOnlyOpIdAndArgs(t *testing.T) {
	schema := parseDestructiveSchema(t)
	required := getDestructiveRequired(t, schema)

	wantRequired := map[string]bool{"op_id": true, "args": true}
	gotRequired := make(map[string]bool, len(required))
	for _, r := range required {
		gotRequired[r] = true
	}

	for want := range wantRequired {
		if !gotRequired[want] {
			t.Errorf("gum.destructive 'required' missing %q (spec §4.1 line ~284)", want)
		}
	}
	for got := range gotRequired {
		if !wantRequired[got] {
			t.Errorf("gum.destructive 'required' has unexpected entry %q — only [\"op_id\",\"args\"] allowed (spec §4.1 line ~284)", got)
		}
	}
	if len(required) != 2 {
		t.Errorf("gum.destructive 'required' has %d entries %v, want exactly 2: [\"op_id\",\"args\"] (spec §4.1 line ~284)", len(required), required)
	}
}

// TestGumDestructiveSchemaAdditionalPropertiesFalse asserts that
// additionalProperties is set to false per spec §4.1 criterion 4.
func TestGumDestructiveSchemaAdditionalPropertiesFalse(t *testing.T) {
	schema := parseDestructiveSchema(t)

	val, ok := schema["additionalProperties"]
	if !ok {
		t.Fatal("gum.destructive schema missing 'additionalProperties' key (spec §4.1 criterion 4)")
	}
	boolVal, ok := val.(bool)
	if !ok {
		t.Fatalf("gum.destructive 'additionalProperties' is %T, want bool false (spec §4.1 criterion 4)", val)
	}
	if boolVal {
		t.Error("gum.destructive 'additionalProperties' is true, want false (spec §4.1 criterion 4)")
	}
}

// TestGumDestructiveAnnotationDestructiveHintTrue asserts that
// TierAMetaToolAnnotations()["gum.destructive"] has ReadOnlyHint=false and
// DestructiveHint=*true per spec §13 annotation table / §4.1 line ~295.
func TestGumDestructiveAnnotationDestructiveHintTrue(t *testing.T) {
	annotations := TierAMetaToolAnnotations()

	ann, ok := annotations["gum.destructive"]
	if !ok || ann == nil {
		t.Fatal("TierAMetaToolAnnotations() has no entry for \"gum.destructive\" (spec §13 — readOnlyHint=false, destructiveHint=true required)")
	}

	if ann.ReadOnlyHint {
		t.Errorf("gum.destructive ReadOnlyHint = true, want false (spec §13 / §4.1 line ~295)")
	}

	if ann.DestructiveHint == nil {
		t.Fatal("gum.destructive DestructiveHint is nil, want *bool(true) (spec §13 — destructiveHint MUST be explicitly set)")
	}
	if !*ann.DestructiveHint {
		t.Errorf("gum.destructive DestructiveHint = *false, want *true (spec §13 / §4.1 line ~295)")
	}
}
