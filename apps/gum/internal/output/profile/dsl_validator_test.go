package profile_test

// dsl_validator_test.go — RED team tests for profile.ValidateRawProfileFile.
// Spec anchor: §5.4 line 676 (CI gate: validate embedded output profiles
// against docs/expression-profile-dsl.json; fail build on any violation).
//
// These tests FAIL to compile until the Green team adds:
//
//	func ValidateRawProfileFile(raw []byte) error
//
// in package profile (internal/output/profile/).

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// ---------------------------------------------------------------------------
// TestValidateRawProfileFileAcceptsGoodFixture
//
// A well-formed profile file matching the first example in
// expression-profile-dsl.md must be accepted (nil error).
// ---------------------------------------------------------------------------

func TestValidateRawProfileFileAcceptsGoodFixture(t *testing.T) {
	good := []byte(`{
		"output_profiles": {
			"_base.list_ops": {
				"format": "toon",
				"strip_nulls": true,
				"collapse_arrays": {"max_items": 20},
				"recovery": "local_artifact"
			},
			"gmail.messages.list.v1": {
				"inherits": "_base.list_ops",
				"field_mask": "nextPageToken,messages(id,threadId)",
				"truncate_strings": {"default_chars": 500, "fields": {"snippet": 180}},
				"on_empty": "No matching messages."
			}
		}
	}`)

	if err := profile.ValidateRawProfileFile(good); err != nil {
		t.Fatalf("known-good fixture was rejected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestValidateRawProfileFileRejectsBadFixture
//
// Three malformed profile documents must each produce a non-nil error. The
// error message should mention the offending field/value so CI authors can fix
// the source file (best-effort string check).
// ---------------------------------------------------------------------------

func TestValidateRawProfileFileRejectsBadFixture(t *testing.T) {
	cases := []struct {
		name        string
		raw         []byte
		wantMention string // substring expected somewhere in the error string
	}{
		{
			// (a) Unknown property inside a profile object.
			// The profile schema has additionalProperties:false, so "weird" must be rejected.
			name:        "unknown_property_in_profile",
			raw:         []byte(`{"output_profiles":{"p":{"format":"toon","weird":"x"}}}`),
			wantMention: "weird",
		},
		{
			// (b) format value "yaml" is not in enum [toon, csv, json, markdown].
			name:        "format_outside_enum",
			raw:         []byte(`{"output_profiles":{"p":{"format":"yaml"}}}`),
			wantMention: "yaml", // enum violation should reference the bad value or "format"
		},
		{
			// (c) collapse_arrays missing required field max_items.
			name:        "collapse_arrays_missing_max_items",
			raw:         []byte(`{"output_profiles":{"p":{"collapse_arrays":{}}}}`),
			wantMention: "max_items",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := profile.ValidateRawProfileFile(tc.raw)
			if err == nil {
				t.Fatalf("case %q: expected a validation error but ValidateRawProfileFile returned nil", tc.name)
			}
			// Best-effort: error should surface the offending field/value.
			if tc.wantMention != "" && !strings.Contains(err.Error(), tc.wantMention) {
				t.Logf("case %q: error does not mention %q (acceptable if error is descriptive in another form): %v",
					tc.name, tc.wantMention, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestValidateRawProfileFileRejectsEmptyFile
//
// An empty profile file ({}) must fail because the schema's anyOf requires at
// least one of output_profiles or override_bindings.
// ---------------------------------------------------------------------------

func TestValidateRawProfileFileRejectsEmptyFile(t *testing.T) {
	if err := profile.ValidateRawProfileFile([]byte(`{}`)); err == nil {
		t.Fatal("empty profile file {} must be rejected (missing output_profiles and override_bindings), but got nil error")
	}
}

// ---------------------------------------------------------------------------
// TestValidateRawProfileFileRejectsInvalidJSON
//
// Non-JSON input must produce a non-nil error with a JSON-parse message.
// ---------------------------------------------------------------------------

func TestValidateRawProfileFileRejectsInvalidJSON(t *testing.T) {
	err := profile.ValidateRawProfileFile([]byte("not json"))
	if err == nil {
		t.Fatal("non-JSON input must be rejected, but got nil error")
	}
	// The error should mention JSON parsing failure.
	msg := err.Error()
	if !strings.Contains(strings.ToLower(msg), "json") &&
		!strings.Contains(strings.ToLower(msg), "invalid") &&
		!strings.Contains(strings.ToLower(msg), "unmarshal") &&
		!strings.Contains(strings.ToLower(msg), "syntax") {
		t.Logf("error message does not mention JSON parsing (got: %q); this may be acceptable if the error is clear", msg)
	}
}
