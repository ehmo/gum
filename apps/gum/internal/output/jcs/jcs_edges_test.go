package jcs_test

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/jcs"
)

// TestJCSCanonicalNilSliceAndMap verifies the nil-slice / nil-map
// short-circuits in validateValue: these are legal JSON values
// (null and the literal `null`) and must not trigger validation
// errors even when reached through interface or pointer wrapping.
func TestJCSCanonicalNilSliceAndMap(t *testing.T) {
	cases := []struct {
		label string
		input any
		want  string
	}{
		{"nil-slice", map[string]any{"v": []any(nil)}, `{"v":null}`},
		{"nil-map", map[string]any{"v": map[string]any(nil)}, `{"v":null}`},
		{"top-level-nil-slice", []any(nil), `null`},
		{"top-level-nil-map", map[string]any(nil), `null`},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got, err := jcs.Marshal(tc.input)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestJCSCanonicalArrayValueValidates verifies the [N]T array branch
// in validateValue (distinct from the slice branch).
func TestJCSCanonicalArrayValueValidates(t *testing.T) {
	input := map[string]any{"v": [3]int{1, 2, 3}}
	got, err := jcs.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != `{"v":[1,2,3]}` {
		t.Errorf("got %q", got)
	}
}

// TestJCSCanonicalArrayContainsInvalid catches the array-element
// recursion path in validateValue: a fixed-size array containing
// an unsupported value (channel) must surface ErrJCSUnsupportedType.
func TestJCSCanonicalArrayContainsInvalid(t *testing.T) {
	input := [1]any{make(chan int)}
	_, err := jcs.Marshal(input)
	if !errors.Is(err, jcs.ErrJCSUnsupportedType) {
		t.Errorf("expected ErrJCSUnsupportedType, got %v", err)
	}
}

// TestJCSCanonicalSliceContainsInvalid catches the slice-element
// recursion path in validateValue.
func TestJCSCanonicalSliceContainsInvalid(t *testing.T) {
	input := []any{1, "ok", make(chan int)}
	_, err := jcs.Marshal(input)
	if !errors.Is(err, jcs.ErrJCSUnsupportedType) {
		t.Errorf("expected ErrJCSUnsupportedType, got %v", err)
	}
}

// TestJCSCanonicalMapContainsInvalid catches the map-value recursion
// path in validateValue.
func TestJCSCanonicalMapContainsInvalid(t *testing.T) {
	input := map[string]any{"v": make(chan int)}
	_, err := jcs.Marshal(input)
	if !errors.Is(err, jcs.ErrJCSUnsupportedType) {
		t.Errorf("expected ErrJCSUnsupportedType, got %v", err)
	}
}

// TestJCSCanonicalStructFieldInvalid catches the struct-field recursion
// path in validateValue.
func TestJCSCanonicalStructFieldInvalid(t *testing.T) {
	type holder struct {
		Ch chan int
	}
	input := holder{Ch: make(chan int)}
	_, err := jcs.Marshal(input)
	if !errors.Is(err, jcs.ErrJCSUnsupportedType) {
		t.Errorf("expected ErrJCSUnsupportedType, got %v", err)
	}
}

// TestJCSCanonicalStructPassesThrough verifies a vanilla struct
// successfully marshals through normalizeToTree (the json.Marshal +
// Decode round-trip path) and emits in JCS form.
func TestJCSCanonicalStructPassesThrough(t *testing.T) {
	type point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	got, err := jcs.Marshal(point{X: 1, Y: 2})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != `{"x":1,"y":2}` {
		t.Errorf("got %q", got)
	}
}

// TestJCSCanonicalFloat32Invalid verifies the float32 NaN path in
// validateValue (separate Go kind from float64).
func TestJCSCanonicalFloat32Invalid(t *testing.T) {
	input := map[string]any{"v": float32(math.NaN())}
	_, err := jcs.Marshal(input)
	if !errors.Is(err, jcs.ErrJCSInvalidNumber) {
		t.Errorf("expected ErrJCSInvalidNumber, got %v", err)
	}
}

// TestJCSCanonicalLargeUint64 catches the ParseUint branch of
// canonicalNumber: a value above math.MaxInt64 must still round-trip
// (uint64 fits, int64 doesn't).
func TestJCSCanonicalLargeUint64(t *testing.T) {
	// json.Number unmarshaling preserves the original token, so
	// marshal-then-canonicalize through a raw JSON literal that
	// json.Marshal can produce. Go can't produce a uint64 > 2^53 via
	// untyped, so use a typed uint64 directly.
	input := map[string]any{"v": uint64(math.MaxUint64)}
	got, err := jcs.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"v":18446744073709551615}`
	if string(got) != want {
		t.Errorf("uint64 max: got %q want %q", got, want)
	}
}

// TestJCSCanonicalUTF16PrefixOrdering catches the loop-exit path in
// utf16Less where one string is a strict prefix of the other.
func TestJCSCanonicalUTF16PrefixOrdering(t *testing.T) {
	// "abc" must sort before "abcd" because all shared code units are
	// equal and len("abc") < len("abcd").
	input := map[string]any{
		"abcd": 2,
		"abc":  1,
	}
	got, err := jcs.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"abc":1,"abcd":2}`
	if string(got) != want {
		t.Errorf("got %q want %q", got, want)
	}
}

// brokenMarshaler returns an error from MarshalJSON, triggering the
// json.Marshal error path inside normalizeToTree (jcs.go lines 124-126).
type brokenMarshaler struct{}

func (brokenMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("boom")
}

// TestJCSCanonicalMarshalJSONError surfaces the normalizeToTree error
// branch when json.Marshal fails.
func TestJCSCanonicalMarshalJSONError(t *testing.T) {
	_, err := jcs.Marshal(brokenMarshaler{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q does not contain %q", err.Error(), "boom")
	}
}

// TestJCSCanonicalPointerToValue verifies the pointer-unwrap loop in
// validateValue completes correctly when the input is a non-nil
// pointer to a supported value.
func TestJCSCanonicalPointerToValue(t *testing.T) {
	v := 42
	got, err := jcs.Marshal(&v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != `42` {
		t.Errorf("pointer: got %q want 42", got)
	}
}
