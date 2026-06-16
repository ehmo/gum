package profile

import (
	"testing"
)

// TestApplyProjectionDefaultArmReturnsValueUnchanged pins
// applyProjection's `default → return v` arm (applier.go:245-246).
// Reached when the input is a scalar (not a map, not an array) —
// projection makes no sense for scalars so the value is passed
// through unchanged. Without this guard the type-switch would fall
// out with no return and the function would return nil.
func TestApplyProjectionDefaultArmReturnsValueUnchanged(t *testing.T) {
	got := applyProjection("scalar-string", []string{"foo"})
	if got != "scalar-string" {
		t.Errorf("applyProjection(scalar)=%v; want passthrough", got)
	}

	gotInt := applyProjection(42, []string{"x"})
	if gotInt != 42 {
		t.Errorf("applyProjection(int)=%v; want passthrough 42", gotInt)
	}
}

// TestApplyProjectionArrayWithNonMapElementPreservesElement pins
// applyProjection's array `else { result[i] = elem }` arm
// (applier.go:240-242). Reached when an array contains a non-map
// element (e.g., a string or number). Projection is a key-filtering
// op; scalars in an array have no keys so they must pass through
// unchanged — otherwise a heterogeneous array would lose its scalars.
func TestApplyProjectionArrayWithNonMapElementPreservesElement(t *testing.T) {
	input := []any{
		map[string]any{"a": 1, "b": 2},
		"scalar-elem",
		42,
	}
	got := applyProjection(input, []string{"a"})
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("applyProjection([])=%T; want []any", got)
	}
	if len(arr) != 3 {
		t.Fatalf("len=%d; want 3 (preserves all elements)", len(arr))
	}
	// Index 0: map → projected to {a:1}
	if m, ok := arr[0].(map[string]any); !ok || m["a"] != 1 || len(m) != 1 {
		t.Errorf("arr[0]=%v; want {a:1}", arr[0])
	}
	// Index 1: scalar string → preserved verbatim
	if arr[1] != "scalar-elem" {
		t.Errorf("arr[1]=%v; want 'scalar-elem' (scalar preserved)", arr[1])
	}
	// Index 2: scalar int → preserved verbatim
	if arr[2] != 42 {
		t.Errorf("arr[2]=%v; want 42 (scalar preserved)", arr[2])
	}
}

// TestCompareValuesBothNilReturnsZero pins compareValues's
// `a==nil && b==nil → 0` arm (applier.go:270-272). Two nil values
// compare equal — without this guard the nil checks below would
// classify nil as "less than non-nil" but two nils have no order.
func TestCompareValuesBothNilReturnsZero(t *testing.T) {
	if got := compareValues(nil, nil); got != 0 {
		t.Errorf("compareValues(nil, nil)=%d; want 0", got)
	}
}

// TestCompareValuesNumericEqualReturnsZero pins compareValues's
// `aIsNum && bIsNum, fa==fb → 0` arm (applier.go:290). Two equal
// numerics must return 0 so SliceStable preserves insertion order
// for equal-keyed records (stable-sort guarantee).
func TestCompareValuesNumericEqualReturnsZero(t *testing.T) {
	if got := compareValues(3.14, 3.14); got != 0 {
		t.Errorf("compareValues(3.14, 3.14)=%d; want 0", got)
	}
	// Cross-type numeric equality (int vs float64) — both go through toFloat.
	if got := compareValues(7, 7.0); got != 0 {
		t.Errorf("compareValues(7, 7.0)=%d; want 0 (cross-type numeric)", got)
	}
}

// TestCompareValuesStringEqualReturnsZero pins compareValues's
// `aIsStr && bIsStr, sa==sb → 0` arm (applier.go:303). Two equal
// strings must return 0 — same stable-sort rationale as numeric.
func TestCompareValuesStringEqualReturnsZero(t *testing.T) {
	if got := compareValues("abc", "abc"); got != 0 {
		t.Errorf("compareValues(\"abc\", \"abc\")=%d; want 0", got)
	}
}

// TestCompareValuesFallbackJSONComparison pins compareValues's
// `json.Marshal fallback → return 0` AND the lexical-compare arms
// (applier.go:307-317). Reached when neither value is purely numeric
// nor purely string — e.g., comparing two maps or two bools. The
// fallback serializes both and compares lexically; equal serializations
// return 0.
func TestCompareValuesFallbackJSONComparison(t *testing.T) {
	// Same map → equal JSON → returns 0.
	a := map[string]any{"x": 1}
	b := map[string]any{"x": 1}
	if got := compareValues(a, b); got != 0 {
		t.Errorf("compareValues(equal maps)=%d; want 0 from JSON fallback", got)
	}

	// Different maps: {x:1} < {x:2} (lexically by JSON).
	c := map[string]any{"x": 2}
	if got := compareValues(a, c); got != -1 {
		t.Errorf("compareValues({x:1}, {x:2})=%d; want -1 (lexical)", got)
	}
	// And the reverse: {x:2} > {x:1} → 1.
	if got := compareValues(c, a); got != 1 {
		t.Errorf("compareValues({x:2}, {x:1})=%d; want 1 (lexical reverse)", got)
	}
}

// TestApplyKeepFieldsScalarPassthrough pins applyKeepFields's
// `default → return v` arm (applier.go:381-382). Reached when input
// is a scalar — keep_fields is a map-key filter, so scalars pass
// through unchanged.
func TestApplyKeepFieldsScalarPassthrough(t *testing.T) {
	if got := applyKeepFields("scalar", []string{"a"}); got != "scalar" {
		t.Errorf("applyKeepFields(scalar)=%v; want passthrough", got)
	}
	if got := applyKeepFields(42, []string{"a"}); got != 42 {
		t.Errorf("applyKeepFields(int)=%v; want passthrough 42", got)
	}
}

// TestApplyDropFieldsScalarPassthrough pins applyDropFields's
// `default → return v` arm (applier.go:410-411). Same rationale as
// keepFields: scalars pass through.
func TestApplyDropFieldsScalarPassthrough(t *testing.T) {
	if got := applyDropFields("scalar", []string{"a"}); got != "scalar" {
		t.Errorf("applyDropFields(scalar)=%v; want passthrough", got)
	}
	if got := applyDropFields(3.14, []string{"a"}); got != 3.14 {
		t.Errorf("applyDropFields(float)=%v; want passthrough 3.14", got)
	}
}

// TestApplyCollapseArraysScalarPassthrough pins applyCollapseArrays's
// `default → return v` arm (applier.go:518-519). Scalars aren't arrays
// or maps — collapse is a no-op on them.
func TestApplyCollapseArraysScalarPassthrough(t *testing.T) {
	spec := &CollapseArraysSpec{MaxItems: 5}
	if got := applyCollapseArrays("scalar", spec); got != "scalar" {
		t.Errorf("applyCollapseArrays(scalar)=%v; want passthrough", got)
	}
	if got := applyCollapseArrays(true, spec); got != true {
		t.Errorf("applyCollapseArrays(bool)=%v; want passthrough true", got)
	}
}

// TestApplyTruncateStringsScalarPassthrough pins applyTruncateStrings's
// `default → return v` arm (applier.go:562-563). Reached when the
// recursive descent hits a non-map, non-array, non-string scalar
// (e.g., a number or bool inside a map). Numbers/bools are passed
// through verbatim; only strings get truncated.
func TestApplyTruncateStringsScalarPassthrough(t *testing.T) {
	spec := &TruncateStringsSpec{DefaultChars: 3}
	if got := applyTruncateStrings(42, spec, ""); got != 42 {
		t.Errorf("applyTruncateStrings(int)=%v; want passthrough 42", got)
	}
	if got := applyTruncateStrings(true, spec, ""); got != true {
		t.Errorf("applyTruncateStrings(bool)=%v; want passthrough true", got)
	}
}
