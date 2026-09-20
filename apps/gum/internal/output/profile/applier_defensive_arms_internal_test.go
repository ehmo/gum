package profile

import (
	"encoding/json"
	"testing"
)

// The recorders are optional: a caller that does not want drop or collapse
// accounting passes nil, and the helpers stay callable on that nil receiver.
// Guarding inside the method keeps the call sites free of nil checks.
func TestNilRecordersAreNoOps(t *testing.T) {
	var dr *dropRecorder
	dr.record("results", "note")
	if dr != nil {
		t.Errorf("dropRecorder = %v, want nil after a no-op record", dr)
	}

	var cr *collapseRecorder
	cr.record("results", "results_omitted_count", 20, 80)
	if cr != nil {
		t.Errorf("collapseRecorder = %v, want nil after a no-op record", cr)
	}

	// A live recorder does store what a nil one discards.
	live := &dropRecorder{}
	live.record("results", "note")
	if _, ok := live.seen["results.note"]; !ok {
		t.Errorf("dropRecorder.seen = %v, want results.note", live.seen)
	}
}

// sort_by orders rows by a numeric field. A json.Number literal that float64
// cannot represent as a finite value has no sort position, so toFloat reports
// the miss instead of saturating to +Inf and reordering the rows around it.
func TestToFloatRejectsUnrepresentableNumbers(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"overflowing_literal", json.Number("1e400"), false},
		{"non_numeric_literal", json.Number("not-a-number"), false},
		{"plain_literal", json.Number("42"), true},
		{"float64", float64(1.5), true},
		{"int", 3, true},
		{"string", "7", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toFloat(tc.in)
			if ok != tc.want {
				t.Fatalf("toFloat(%v) ok = %v, want %v", tc.in, ok, tc.want)
			}
			if !ok && got != 0 {
				t.Errorf("toFloat(%v) = %v on the miss arm, want 0", tc.in, got)
			}
		})
	}
}

// isEmptyShape decides whether shaping left a result set behind. An empty slice
// counts as empty even though the applier reaches that case through the record
// array instead.
func TestIsEmptyShapeClassifiesEveryKind(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"nil", nil, true},
		{"empty_slice", []any{}, true},
		{"populated_slice", []any{1}, false},
		{"empty_map", map[string]any{}, true},
		{"populated_map", map[string]any{"a": 1}, false},
		{"scalar", "text", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEmptyShape(tc.in); got != tc.want {
				t.Errorf("isEmptyShape(%#v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// The occurrence-count write-back indexes back into the result slice. It skips
// any survivor that is not a map, because a non-map row has nowhere to carry
// the count.
func TestApplyDedupeSkipsNonMapSurvivors(t *testing.T) {
	arr := []any{
		map[string]any{"id": "a"},
		map[string]any{"id": "a"},
		"loose scalar",
		"loose scalar",
	}

	got, removed := applyDedupe(arr, &DedupeSpec{By: []string{"id"}})

	// The two scalars have no key fields, so both pass through unchanged and
	// only the duplicate map row is collapsed.
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if len(got) != 3 {
		t.Fatalf("rows = %d, want 3: %v", len(got), got)
	}
	survivor, ok := got[0].(map[string]any)
	if !ok {
		t.Fatalf("got[0] = %T, want map[string]any", got[0])
	}
	if survivor[occurrenceCountKey] != 2 {
		t.Errorf("%s = %v, want 2", occurrenceCountKey, survivor[occurrenceCountKey])
	}
	if got[1] != "loose scalar" || got[2] != "loose scalar" {
		t.Errorf("scalars = %v, %v; non-map rows pass through unchanged", got[1], got[2])
	}
}
