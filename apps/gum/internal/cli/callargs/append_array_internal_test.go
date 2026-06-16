package callargs

import (
	"reflect"
	"testing"
)

// TestAppendArrayShapes pins the two branches: existing []any extends in
// place; anything else wraps both values into a fresh []any. Both branches
// must preserve element order — CLI repeated-key arrays are positional.
func TestAppendArrayShapes(t *testing.T) {
	cases := []struct {
		name     string
		existing any
		value    any
		want     []any
	}{
		{"extend_existing_array", []any{"a"}, "b", []any{"a", "b"}},
		{"extend_empty_array", []any{}, "x", []any{"x"}},
		{"wrap_scalar", "first", "second", []any{"first", "second"}},
		{"wrap_mixed_types", 42, "two", []any{42, "two"}},
		{"wrap_nil_existing", nil, "v", []any{nil, "v"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := appendArray(tc.existing, tc.value)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got=%v; want %v", got, tc.want)
			}
		})
	}
}
