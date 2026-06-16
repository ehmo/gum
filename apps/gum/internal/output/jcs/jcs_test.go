package jcs_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/output/jcs"
)

// TestJCSCanonicalKeySorting verifies that object keys are sorted in
// lexicographic order by UTF-16 code units as required by RFC 8785 §3.2.3.
func TestJCSCanonicalKeySorting(t *testing.T) {
	input := map[string]any{
		"b": 1,
		"a": 2,
	}
	got, err := jcs.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	want := `{"a":2,"b":1}`
	if string(got) != want {
		t.Errorf("key sorting: got %q, want %q", got, want)
	}
}

// TestJCSCanonicalNestedSorting verifies that key sorting recurses into
// nested objects.
func TestJCSCanonicalNestedSorting(t *testing.T) {
	input := map[string]any{
		"z": map[string]any{
			"y": "one",
			"x": "two",
		},
		"a": map[string]any{
			"d": 4,
			"c": 3,
		},
	}
	got, err := jcs.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	want := `{"a":{"c":3,"d":4},"z":{"x":"two","y":"one"}}`
	if string(got) != want {
		t.Errorf("nested sorting: got %q, want %q", got, want)
	}
}

// TestJCSCanonicalArrayOrder verifies that arrays preserve insertion order
// and are NOT sorted (RFC 8785 §3.2.2).
func TestJCSCanonicalArrayOrder(t *testing.T) {
	input := map[string]any{
		"items": []any{3, 1, 2, "b", "a"},
	}
	got, err := jcs.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	want := `{"items":[3,1,2,"b","a"]}`
	if string(got) != want {
		t.Errorf("array order: got %q, want %q", got, want)
	}
}

// TestJCSCanonicalNumberRepresentation verifies the canonical number
// serialization rules from RFC 8785 §3.2.2.3: integers stay integers,
// exponent notation is expanded, trailing zeros are dropped, zero is zero.
func TestJCSCanonicalNumberRepresentation(t *testing.T) {
	cases := []struct {
		label string
		input any
		want  string
	}{
		{"int-literal", map[string]any{"v": 1}, `{"v":1}`},
		{"simple-float", map[string]any{"v": 1.5}, `{"v":1.5}`},
		{"1e2", map[string]any{"v": 1e2}, `{"v":100}`},
		{"1.5e-2", map[string]any{"v": 1.5e-2}, `{"v":0.015}`},
		{"trailing-zero", map[string]any{"v": 1.10}, `{"v":1.1}`},
		{"zero-int", map[string]any{"v": 0}, `{"v":0}`},
		{"zero-float", map[string]any{"v": 0.0}, `{"v":0}`},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got, err := jcs.Marshal(tc.input)
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("number repr: got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestJCSCanonicalStringEscaping verifies RFC 8785 §3.2.2.2 string rules:
// control chars U+0000..U+001F must be emitted as \uXXXX (lower-case hex);
// '"' and '\' get backslash escapes; all other codepoints pass as UTF-8.
func TestJCSCanonicalStringEscaping(t *testing.T) {
	cases := []struct {
		label string
		input string
		want  string
	}{
		{"double-quote", "say \"hi\"", "{\"v\":\"say \\\"hi\\\"\"}"},
		{"backslash", "a\\b", "{\"v\":\"a\\\\b\"}"},
		{"newline", "\n", "{\"v\":\"\\u000a\"}"},
		{"tab", "\t", "{\"v\":\"\\u0009\"}"},
		{"nul", "\x00", "{\"v\":\"\\u0000\"}"},
		{"cr", "\r", "{\"v\":\"\\u000d\"}"},
		{"backspace", "\x08", "{\"v\":\"\\u0008\"}"},
		{"unit-sep", "\x1f", "{\"v\":\"\\u001f\"}"},
		{"space", " ", "{\"v\":\" \"}"},
		{"utf8-latin", "café", "{\"v\":\"café\"}"},
		{"ascii", "hello world", "{\"v\":\"hello world\"}"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			input := map[string]any{"v": tc.input}
			got, err := jcs.Marshal(input)
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("string escaping [%s]:\n  got  %q\n  want %q", tc.label, got, tc.want)
			}
		})
	}
}

// TestJCSCanonicalNullBoolean verifies that null, true, and false are
// serialized to their literal JSON forms.
func TestJCSCanonicalNullBoolean(t *testing.T) {
	cases := []struct {
		label string
		input any
		want  string
	}{
		{"null", map[string]any{"v": nil}, `{"v":null}`},
		{"true", map[string]any{"v": true}, `{"v":true}`},
		{"false", map[string]any{"v": false}, `{"v":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got, err := jcs.Marshal(tc.input)
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("null/bool: got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestJCSCanonicalBytesAreStable verifies that calling Marshal twice on the
// same input returns byte-identical results.
func TestJCSCanonicalBytesAreStable(t *testing.T) {
	input := map[string]any{
		"z": 3,
		"a": []any{1, "two", nil, false},
		"m": map[string]any{
			"y": 9,
			"x": "hello",
		},
	}
	first, err := jcs.Marshal(input)
	if err != nil {
		t.Fatalf("first Marshal error: %v", err)
	}
	second, err := jcs.Marshal(input)
	if err != nil {
		t.Fatalf("second Marshal error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("stability: first=%q second=%q", first, second)
	}
}

// TestJCSCanonicalErrorOnNaN verifies that NaN, +Inf, and -Inf all return an
// error. JSON has no representation for these values (RFC 8785 §3.2.2.3).
func TestJCSCanonicalErrorOnNaN(t *testing.T) {
	cases := []struct {
		label string
		input any
	}{
		{"NaN", map[string]any{"v": math.NaN()}},
		{"+Inf", map[string]any{"v": math.Inf(1)}},
		{"-Inf", map[string]any{"v": math.Inf(-1)}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := jcs.Marshal(tc.input)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tc.label)
			}
		})
	}
}

// TestJCSCanonicalErrorOnUnsupportedType verifies that channels, functions,
// and complex numbers produce an error rather than silently dropping or
// panicking.
func TestJCSCanonicalErrorOnUnsupportedType(t *testing.T) {
	ch := make(chan int)
	fn := func() {}
	cx := complex(1.0, 2.0)

	cases := []struct {
		label string
		input any
	}{
		{"channel", map[string]any{"v": ch}},
		{"func", map[string]any{"v": fn}},
		{"complex", map[string]any{"v": cx}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := jcs.Marshal(tc.input)
			if err == nil {
				t.Errorf("expected error for %s, got nil", tc.label)
			}
		})
	}
}

// TestJCSGoldenFiles reads *.input.json / *.canonical.json pairs from
// testdata/ and asserts that Marshal(parsed input) equals the canonical bytes.
func TestJCSGoldenFiles(t *testing.T) {
	inputs, err := filepath.Glob("testdata/*.input.json")
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(inputs) == 0 {
		t.Fatal("no golden input files found in testdata/")
	}
	for _, inputPath := range inputs {
		base := inputPath[:len(inputPath)-len(".input.json")]
		canonicalPath := base + ".canonical.json"

		t.Run(filepath.Base(base), func(t *testing.T) {
			inputBytes, err := os.ReadFile(inputPath)
			if err != nil {
				t.Fatalf("read input: %v", err)
			}
			canonicalBytes, err := os.ReadFile(canonicalPath)
			if err != nil {
				t.Fatalf("read canonical: %v", err)
			}

			var parsed any
			if err := json.Unmarshal(inputBytes, &parsed); err != nil {
				t.Fatalf("parse input JSON: %v", err)
			}

			got, err := jcs.Marshal(parsed)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			// Trim trailing newline from canonical file if present.
			canonical := bytes.TrimRight(canonicalBytes, "\n")
			if !bytes.Equal(got, canonical) {
				t.Errorf("golden mismatch:\n  got:  %s\n  want: %s", got, canonical)
			}
		})
	}
}
