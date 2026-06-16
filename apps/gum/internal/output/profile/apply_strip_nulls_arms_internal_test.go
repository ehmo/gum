package profile

import (
	"reflect"
	"testing"
)

// TestApplyStripNullsSliceArm pins the []any switch arm: applyStripNulls
// must recurse into every element of an input slice (so nested map members
// also get scrubbed) and return a new slice of the same length. Without
// this branch a top-level array payload would short-circuit to the
// default arm and skip stripping entirely.
func TestApplyStripNullsSliceArm(t *testing.T) {
	in := []any{
		map[string]any{"keep": "v", "drop": ""},
		map[string]any{"only": ""}, // becomes empty map; NOT removed from slice (slice elements aren't pruned)
		"hello",
	}
	got := applyStripNulls(in)
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("got %T; want []any", got)
	}
	if len(arr) != 3 {
		t.Fatalf("len(arr)=%d; want 3 (slice arm preserves length)", len(arr))
	}
	// Element 0: drop was scrubbed, keep remains.
	m0, _ := arr[0].(map[string]any)
	if !reflect.DeepEqual(m0, map[string]any{"keep": "v"}) {
		t.Errorf("arr[0]=%v; want {keep:v}", m0)
	}
	// Element 1: the inner map became empty; the slice arm itself does NOT
	// prune slice members, only map members.
	m1, _ := arr[1].(map[string]any)
	if len(m1) != 0 {
		t.Errorf("arr[1]=%v; want empty map (members pruned, slice not)", m1)
	}
	// Element 2: scalar passes through via the default arm.
	if arr[2] != "hello" {
		t.Errorf("arr[2]=%v; want 'hello'", arr[2])
	}
}

// TestApplyStripNullsDefaultArmPassthrough pins the default switch arm:
// scalars (string, number, bool, nil) MUST pass through untouched. A
// regression that recursed on scalars would either panic or coerce them.
func TestApplyStripNullsDefaultArmPassthrough(t *testing.T) {
	cases := []any{
		"plain string",
		42,
		3.14,
		true,
		false,
		nil,
	}
	for _, c := range cases {
		if got := applyStripNulls(c); !reflect.DeepEqual(got, c) {
			t.Errorf("applyStripNulls(%v)=%v; want passthrough", c, got)
		}
	}
}
