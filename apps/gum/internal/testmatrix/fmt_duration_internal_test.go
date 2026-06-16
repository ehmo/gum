package testmatrix

import (
	"testing"
	"time"
)

// TestFmtDurationShapes pins the sub-second vs. second branches —
// the report header parses this format and the millisecond branch
// renders an integer (no decimal) while the seconds branch renders
// two-decimal seconds. Don't churn the format without rev'ing the
// report parser at the same time.
func TestFmtDurationShapes(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want string
	}{
		{"sub_second_zero", 0, "0ms"},
		{"sub_second_999ms", 999 * time.Millisecond, "999ms"},
		{"sub_second_500us_rounds_to_zero_ms", 500 * time.Microsecond, "0ms"},
		{"exactly_one_second", time.Second, "1.00s"},
		{"fractional_second", 1500 * time.Millisecond, "1.50s"},
		{"multi_second", 12345 * time.Millisecond, "12.35s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmtDuration(tc.in); got != tc.want {
				t.Errorf("got=%q; want %q", got, tc.want)
			}
		})
	}
}
