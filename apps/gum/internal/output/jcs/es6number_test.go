package jcs_test

import (
	"testing"

	"github.com/ehmo/gum/internal/output/jcs"
)

// TestJCSNumbersFollowES6ToString pins RFC 8785 §3.2.2.3, which serializes
// numbers with the ECMAScript Number::toString algorithm rather than with any
// convenient host formatter. Go's `strconv` 'g' verb was close enough for the
// small decimals the earlier tests covered and wrong everywhere else: it
// switches to an exponent at 1e7, pads the exponent to two digits, and never
// expands an exponent back into digits. ES6 expands every value whose decimal
// exponent lands in (-7, 21], pads nothing, and only then falls back to an
// exponent.
//
// The divergence matters because args_canonical feeds the semantic cache key,
// the audit row, the confirmation-token binding hash and the tee artifact
// hash. gum agreed with itself before this gate; it disagreed with every other
// RFC 8785 implementation on the values below (bead gum-xvy9).
func TestJCSNumbersFollowES6ToString(t *testing.T) {
	cases := []struct {
		label string
		input float64
		want  string
	}{
		// Rule 1 (k <= n <= 21): digits, then n-k zeros. Go emitted "1e+07".
		{"ten-million", 1e7, "10000000"},
		// The upper edge of rule 1. n is exactly 21. Go emitted "1e+20".
		{"1e20-expands", 1e20, "100000000000000000000"},
		// One past the edge: n is 22, so rule 4 applies and both agree.
		{"1e21-keeps-exponent", 1e21, "1e+21"},
		// Rule 1 with a long mantissa: 17 digits, then 4 zeros.
		{"long-mantissa-expands", 1.2345678901234568e20, "123456789012345680000"},
		// Rule 2 (0 < n <= 21): decimal point inside the digits.
		{"fraction-in-digits", 1234.5678, "1234.5678"},
		// Rule 3 (-6 < n <= 0): leading "0." and -n zeros. Go emitted "1e-06".
		{"one-millionth-expands", 1e-6, "0.000001"},
		// The lower edge of rule 3. n is -5 here and -6 for the next case.
		{"rule3-edge", 1.5e-6, "0.0000015"},
		// Rule 4 (k == 1): no exponent padding. Go emitted "1e-07".
		{"ten-millionth-unpadded", 1e-7, "1e-7"},
		// Rule 5 (k > 1): one digit, ".", the rest, then the exponent.
		{"rule5-negative-exponent", 1.5e-7, "1.5e-7"},
		{"rule5-positive-exponent", 9.999999999999997e22, "9.999999999999997e+22"},
		// Smallest subnormal. Three exponent digits, still unpadded.
		{"smallest-subnormal", 5e-324, "5e-324"},
		// Negative values carry the sign and canonicalize the magnitude.
		{"negative-expands", -1e7, "-10000000"},
		// RFC 8785 §3.2.2.3 serializes negative zero as "0".
		{"negative-zero", negZero(), "0"},
		// Values the earlier tests already covered, kept so a rewrite of the
		// rules cannot regress them.
		{"hundred", 1e2, "100"},
		{"fifteen-thousandths", 1.5e-2, "0.015"},
		{"half", 0.5, "0.5"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			got, err := jcs.Marshal(map[string]any{"v": tc.input})
			if err != nil {
				t.Fatalf("Marshal(%v) returned error: %v", tc.input, err)
			}

			want := `{"v":` + tc.want + `}`
			if string(got) != want {
				t.Errorf("Marshal(%v) = %s; want %s", tc.input, got, want)
			}
		})
	}
}

// negZero returns -0.0 without a constant expression the compiler folds to +0.
func negZero() float64 {
	zero := 0.0

	return -zero
}
