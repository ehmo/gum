// Regression test for the shaping defect found in the 2026-09 whole-repo review.
package profile

import (
	"reflect"
	"testing"
)

// applier.go: applyCollapseArrays sliced arr[:spec.MaxItems] with no lower
// bound, so a negative max_items panicked with a slice-bounds-out-of-range.
// The DSL validator rejects a negative value, but the applier is also called
// with specs built in code (the MCP meta-tool profiles), and a panic inside
// `gum mcp --stdio` ends the session rather than the one call. The array is
// returned untouched: a cap with no meaning caps nothing.
func TestCollapseArraysNegativeMaxItemsIsInert(t *testing.T) {
	in := []any{"a", "b", "c"}

	for _, max := range []int{-1, -100} {
		spec := &CollapseArraysSpec{MaxItems: max}
		got := applyCollapseArrays(in, spec, &collapseRecorder{})
		if !reflect.DeepEqual(got, any(in)) {
			t.Errorf("max_items=%d: got %#v; want the input array unchanged", max, got)
		}
	}
}

// applier.go: max_items=0 is a real, documented setting (spec §9.1
// intentional_zero_max_items), so the guard must not swallow it.
func TestCollapseArraysZeroMaxItemsStillCollapses(t *testing.T) {
	got := applyCollapseArrays([]any{"a", "b"}, &CollapseArraysSpec{MaxItems: 0}, &collapseRecorder{})
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T; want the {items, omitted_count} wrapper", got)
	}
	if n, _ := m["omitted_count"].(int); n != 2 {
		t.Errorf("omitted_count = %v; want 2", m["omitted_count"])
	}
}
