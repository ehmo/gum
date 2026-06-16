package toon

import "testing"

// TestIsZeroValInt32And32 pins the `int32` and `uint32` type-switch
// arms. The TOON encoder calls isZeroVal to elide zero-valued numerics
// from sparse maps (spec gum-toon); int32 and uint32 are explicit
// arms in the switch because typed encoders (gRPC, protobuf) commonly
// surface those bit-width-specific types — without these arms they'd
// fall through to `default: return false` and render as "0" rather
// than being elided like their int64/uint64 counterparts.
func TestIsZeroValInt32And32(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want bool
	}{
		{"int32_zero", int32(0), true},
		{"int32_nonzero", int32(7), false},
		{"uint32_zero", uint32(0), true},
		{"uint32_nonzero", uint32(7), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isZeroVal(tc.v); got != tc.want {
				t.Errorf("isZeroVal(%T %v)=%v; want %v", tc.v, tc.v, got, tc.want)
			}
		})
	}
}
