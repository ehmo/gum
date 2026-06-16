package bench

import "testing"

// TestHasComplexValueNestedMapReturnsTrue pins the `case map[string]any
// → return true` arm. A nested map under any key MUST short-circuit to
// true so flattenScalars leaves the parent key intact — flattening
// beyond one level would inflate the dot-path column count without
// preserving tabular structure, which is the whole point of the
// flatten-only-when-scalar guard in release_savings.shapeParallel.
func TestHasComplexValueNestedMapReturnsTrue(t *testing.T) {
	m := map[string]any{
		"outer": map[string]any{"deeper": "x"},
	}
	if !hasComplexValue(m) {
		t.Error("hasComplexValue(nested map)=false; want true (must short-circuit on map[string]any)")
	}
}
