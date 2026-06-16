package profile_test

// dsl_schema_test.go exercises docs/expression-profile-dsl.json as the
// structural validator for expression-profile files (JSON Schema 2020-12).
// Spec anchor: §5.4 line 676, §9.1 line 1966.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// schemaPath returns the absolute path to docs/expression-profile-dsl.json
// relative to the test file's location (internal/output/profile/../../..).
func schemaPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile → .../apps/gum/internal/output/profile/dsl_schema_test.go
	// schema   → .../docs/expression-profile-dsl.json  (5 levels up from the test file)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "docs")
	return filepath.Join(repoRoot, "expression-profile-dsl.json")
}

// loadAndCompileSchema reads the schema file from disk, parses it, and
// compiles it into a *jsonschema.Resolved ready for validation calls.
// It calls t.Fatal on any error so callers can proceed without error handling.
func loadAndCompileSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()

	path := schemaPath(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loadAndCompileSchema: cannot read schema file %s: %v", path, err)
	}

	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("loadAndCompileSchema: schema is not valid JSON: %v", err)
	}

	resolved, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("loadAndCompileSchema: schema.Resolve failed: %v", err)
	}

	return resolved
}

// validateProfileJSON compiles the schema once, then validates a JSON-encoded
// profile-file document. Returns the validation error (nil = success).
func validateProfileJSON(t *testing.T, rs *jsonschema.Resolved, doc any) error {
	t.Helper()
	return rs.Validate(doc)
}

// mustParseJSON is a small test helper that unmarshals JSON into an any value.
func mustParseJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("mustParseJSON: %v", err)
	}
	return v
}

// ---------------------------------------------------------------------------
// TestExpressionProfileDSLSchemaParses
//
// Verify that the schema file exists, is valid JSON, contains the 2020-12
// $schema URI, and compiles without error.
// ---------------------------------------------------------------------------

func TestExpressionProfileDSLSchemaParses(t *testing.T) {
	path := schemaPath(t)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("schema file missing at %s: %v\n"+
			"(RED: file must be created by the Green team)", path, err)
	}

	// Must be valid JSON.
	var top any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("schema file is not valid JSON: %v", err)
	}

	// Must be a JSON object.
	obj, ok := top.(map[string]any)
	if !ok {
		t.Fatalf("schema root must be a JSON object, got %T", top)
	}

	// Must declare the 2020-12 $schema URI.
	const want2020 = "https://json-schema.org/draft/2020-12/schema"
	schemaURI, _ := obj["$schema"].(string)
	if schemaURI != want2020 {
		t.Fatalf("$schema must be %q, got %q", want2020, schemaURI)
	}

	// Must compile via jsonschema-go.
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("json.Unmarshal into jsonschema.Schema failed: %v", err)
	}
	if _, err := s.Resolve(nil); err != nil {
		t.Fatalf("schema.Resolve failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestExpressionProfileDSLValidatesGoodFixture
//
// A well-formed profile file matching the first example in expression-profile-dsl.md
// must pass schema validation with zero errors.
// ---------------------------------------------------------------------------

func TestExpressionProfileDSLValidatesGoodFixture(t *testing.T) {
	rs := loadAndCompileSchema(t)

	// Mirrors the DSL doc example:
	//   [output_profiles."_base.list_ops"]
	//   format = "toon"; strip_nulls = true; collapse_arrays = {max_items=20}
	//   recovery = "local_artifact"
	//
	//   [output_profiles."gmail.messages.list.v1"]
	//   inherits = "_base.list_ops"
	//   field_mask = "nextPageToken,messages(id,threadId)"
	//   truncate_strings = {default_chars=500, fields={snippet=180}}
	//   on_empty = "No matching messages."
	good := mustParseJSON(t, `{
		"output_profiles": {
			"_base.list_ops": {
				"format":         "toon",
				"strip_nulls":    true,
				"collapse_arrays": {"max_items": 20},
				"recovery":       "local_artifact"
			},
			"gmail.messages.list.v1": {
				"inherits":          "_base.list_ops",
				"field_mask":        "nextPageToken,messages(id,threadId)",
				"truncate_strings":  {"default_chars": 500, "fields": {"snippet": 180}},
				"on_empty":          "No matching messages."
			}
		}
	}`)

	if err := validateProfileJSON(t, rs, good); err != nil {
		t.Fatalf("known-good fixture failed schema validation: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestExpressionProfileDSLRejectsBadFixture
//
// Three known-bad profile documents must each produce a validation error.
// ---------------------------------------------------------------------------

func TestExpressionProfileDSLRejectsBadFixture(t *testing.T) {
	rs := loadAndCompileSchema(t)

	cases := []struct {
		name string
		doc  string
	}{
		{
			// (a) Unknown property inside a profile object — must fail because
			// profile objects require additionalProperties: false.
			name: "unknown_property_in_profile",
			doc: `{
				"output_profiles": {
					"bad_profile": {
						"format_unknown": "toon"
					}
				}
			}`,
		},
		{
			// (b) format value "yaml" is not in the enum [toon, csv, json, markdown].
			name: "format_not_in_enum",
			doc: `{
				"output_profiles": {
					"bad_profile": {
						"format": "yaml"
					}
				}
			}`,
		},
		{
			// (c) collapse_arrays missing required max_items field.
			name: "collapse_arrays_missing_max_items",
			doc: `{
				"output_profiles": {
					"bad_profile": {
						"collapse_arrays": {}
					}
				}
			}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := mustParseJSON(t, tc.doc)
			err := validateProfileJSON(t, rs, doc)
			if err == nil {
				t.Fatalf("case %q: expected a validation error but schema accepted the document", tc.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestExpressionProfileDSLOnEmptyHasNoMaxLength
//
// Per spec §9 / expression-profile-dsl.md: the JSON Schema deliberately omits
// maxLength on on_empty because JSON Schema maxLength counts raw codepoints
// without NFC normalization. A 1000-character on_empty value must NOT trigger
// a schema error (the Go validator handles the NFC + 500-codepoint check).
// ---------------------------------------------------------------------------

func TestExpressionProfileDSLOnEmptyHasNoMaxLength(t *testing.T) {
	rs := loadAndCompileSchema(t)

	longOnEmpty := strings.Repeat("x", 1000) // 1000 chars — exceeds 500 on purpose.

	doc := map[string]any{
		"output_profiles": map[string]any{
			"test_profile": map[string]any{
				"on_empty": longOnEmpty,
			},
		},
	}

	if err := validateProfileJSON(t, rs, doc); err != nil {
		t.Fatalf("schema rejected a 1000-char on_empty value, but it must NOT apply maxLength "+
			"(NFC + 500 codepoint check is the Go validator's job, not the JSON Schema's): %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestExpressionProfileDSLAtLeastOneTopLevelTable
//
// Per DSL doc validation rule 6: a profile file MUST contain at least one of
// output_profiles or override_bindings. An empty object must fail.
// Files with only one of those tables must pass.
// ---------------------------------------------------------------------------

func TestExpressionProfileDSLAtLeastOneTopLevelTable(t *testing.T) {
	rs := loadAndCompileSchema(t)

	t.Run("empty_object_must_fail", func(t *testing.T) {
		doc := mustParseJSON(t, `{}`)
		if err := validateProfileJSON(t, rs, doc); err == nil {
			t.Fatal("empty top-level object must fail validation (missing output_profiles and override_bindings)")
		}
	})

	t.Run("only_output_profiles_must_pass", func(t *testing.T) {
		doc := mustParseJSON(t, `{"output_profiles": {"foo": {}}}`)
		if err := validateProfileJSON(t, rs, doc); err != nil {
			t.Fatalf("file with only output_profiles must be valid, got error: %v", err)
		}
	})

	t.Run("only_override_bindings_must_pass", func(t *testing.T) {
		doc := mustParseJSON(t, `{"override_bindings": {"x.y": "p"}}`)
		if err := validateProfileJSON(t, rs, doc); err != nil {
			t.Fatalf("file with only override_bindings must be valid, got error: %v", err)
		}
	})
}
