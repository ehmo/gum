package profile

import "testing"

// TestToFloatShapes pins the numeric-coercion matrix used by min/max/eq
// operators on JSON numbers. JSON unmarshals all numbers as float64 in
// the encoding/json default — the int/int32/int64 cases catch values
// that flow in from Go-side handlers (catalog metrics, plugin params)
// without round-tripping through json.Marshal first. The default branch
// keeps non-numeric values from silently coercing to zero.
func TestToFloatShapes(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want float64
		ok   bool
	}{
		{"float64", float64(1.5), 1.5, true},
		{"int", 7, 7, true},
		{"int64", int64(-3), -3, true},
		{"int32", int32(42), 42, true},
		{"string_rejected", "1.5", 0, false},
		{"bool_rejected", true, 0, false},
		{"nil_rejected", nil, 0, false},
		{"slice_rejected", []float64{1.0}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toFloat(tc.in)
			if ok != tc.ok {
				t.Errorf("ok=%v; want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("got=%v; want %v", got, tc.want)
			}
		})
	}
}
