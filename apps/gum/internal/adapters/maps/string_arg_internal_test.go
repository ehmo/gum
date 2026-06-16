package maps

import "testing"

// TestStringArgBranches covers nil/missing/present/wrong-type — the four
// shape outcomes Execute must tolerate when callers pass partial args.
func TestStringArgBranches(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		key  string
		want string
	}{
		{"nil_args", nil, "address", ""},
		{"missing_key", map[string]any{}, "address", ""},
		{"present_string", map[string]any{"address": "1600 Amphitheatre"}, "address", "1600 Amphitheatre"},
		{"wrong_type", map[string]any{"address": 42}, "address", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stringArg(tc.args, tc.key); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}
