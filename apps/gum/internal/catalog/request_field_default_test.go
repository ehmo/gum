package catalog_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestRequestFieldHasDefault covers the presence predicate the dispatcher gates
// on: an empty Default string means "no default declared", not "default to the
// empty string".
func TestRequestFieldHasDefault(t *testing.T) {
	if (catalog.RequestField{Name: "a"}).HasDefault() {
		t.Error("empty Default reported as declared")
	}
	if !(catalog.RequestField{Name: "a", Default: "x"}).HasDefault() {
		t.Error("non-empty Default reported as absent")
	}
}

// TestRequestFieldDefaultValueDecodes checks that each declared type decodes to
// the Go value the dispatcher injects, and that the JSON encoding of that value
// matches the declared type. A wrongly-typed injection would reach the upstream
// API as a 400 the caller never asked for.
func TestRequestFieldDefaultValueDecodes(t *testing.T) {
	cases := []struct {
		name  string
		field catalog.RequestField
		want  string // JSON encoding of the decoded default
	}{
		{"string", catalog.RequestField{Name: "q", Type: "string", Default: "hello"}, `"hello"`},
		{"empty type is string", catalog.RequestField{Name: "q", Default: "hello"}, `"hello"`},
		{"unknown type is string", catalog.RequestField{Name: "q", Type: "quaternion", Default: "hello"}, `"hello"`},
		{"integer", catalog.RequestField{Name: "rowLimit", Type: "integer", Default: "1000"}, `1000`},
		{"integer zero", catalog.RequestField{Name: "startRow", Type: "integer", Default: "0"}, `0`},
		{"negative integer", catalog.RequestField{Name: "offset", Type: "integer", Default: "-5"}, `-5`},
		{"number", catalog.RequestField{Name: "ratio", Type: "number", Default: "0.25"}, `0.25`},
		{"boolean false", catalog.RequestField{Name: "adult", Type: "boolean", Default: "false"}, `false`},
		{"boolean true", catalog.RequestField{Name: "adult", Type: "boolean", Default: "true"}, `true`},
		{"array wraps a bare scalar", catalog.RequestField{Name: "geo", Type: "array", ItemType: "string", Default: "geoTargetConstants/2840"}, `["geoTargetConstants/2840"]`},
		{"array of integers wraps typed", catalog.RequestField{Name: "ids", Type: "array", ItemType: "integer", Default: "7"}, `[7]`},
		{"array accepts a JSON literal", catalog.RequestField{Name: "geo", Type: "array", ItemType: "string", Default: `["a","b"]`}, `["a","b"]`},
		{"array literal with leading space", catalog.RequestField{Name: "geo", Type: "array", ItemType: "string", Default: `  ["a"]`}, `["a"]`},
		{"object", catalog.RequestField{Name: "opts", Type: "object", Default: `{"k":1}`}, `{"k":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.field.DefaultValue()
			if err != nil {
				t.Fatalf("DefaultValue: %v", err)
			}
			enc, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal %#v: %v", got, err)
			}
			if string(enc) != tc.want {
				t.Fatalf("got %s, want %s", enc, tc.want)
			}
		})
	}
}

// TestRequestFieldDefaultValueRejectsMistypedDefault proves a curator's typo is
// an error rather than a silently mistyped value on the wire. The catalog
// invariant test in internal/embedded turns this into a build-time failure.
func TestRequestFieldDefaultValueRejectsMistypedDefault(t *testing.T) {
	cases := []struct {
		name  string
		field catalog.RequestField
	}{
		{"integer from words", catalog.RequestField{Name: "rowLimit", Type: "integer", Default: "one thousand"}},
		{"integer from float", catalog.RequestField{Name: "rowLimit", Type: "integer", Default: "10.5"}},
		{"number from words", catalog.RequestField{Name: "ratio", Type: "number", Default: "half"}},
		{"boolean from words", catalog.RequestField{Name: "adult", Type: "boolean", Default: "yes please"}},
		{"object from scalar", catalog.RequestField{Name: "opts", Type: "object", Default: "nope"}},
		{"array literal malformed", catalog.RequestField{Name: "geo", Type: "array", ItemType: "string", Default: `["a"`}},
		{"array element mistyped", catalog.RequestField{Name: "ids", Type: "array", ItemType: "integer", Default: "abc"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.field.DefaultValue()
			if err == nil {
				t.Fatalf("DefaultValue accepted %q as %s: %#v", tc.field.Default, tc.field.Type, got)
			}
		})
	}
}

// TestRequestFieldDefaultValueNoDefault documents the sentinel a caller can
// match on, so "no default" is distinguishable from "a default that failed to
// decode".
func TestRequestFieldDefaultValueNoDefault(t *testing.T) {
	_, err := (catalog.RequestField{Name: "q", Type: "string"}).DefaultValue()
	if !errors.Is(err, catalog.ErrNoDefaultDeclared) {
		t.Fatalf("got %v, want ErrNoDefaultDeclared", err)
	}
}
