package plugins

import "testing"

// TestIntOfShapes covers the supervisor's JSON-number coercion. The
// helper accepts the three concrete numeric types that flow back from
// the SDK (int from Go literals, float64 from json.Unmarshal, int64
// from typed catalog fields) and folds anything else to zero so a
// malformed manifest can't crash the supervisor backoff math.
func TestIntOfShapes(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"int", 7, 7},
		{"int64", int64(42), 42},
		{"float64_whole", float64(5), 5},
		{"float64_fraction_truncates", 1.9, 1},
		{"string_rejected", "5", 0},
		{"bool_rejected", true, 0},
		{"nil_rejected", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := intOf(tc.in); got != tc.want {
				t.Errorf("got=%d; want %d", got, tc.want)
			}
		})
	}
}
