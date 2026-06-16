package adapters

import "testing"

// TestClampRetryAfterShapes pins the three branches: negative → 0
// (defends against clock skew when computing Retry-After from a
// past date); within-bounds → identity; over-ceiling → capped at
// maxRetryAfterSeconds. The 300s ceiling is spec-mandated and the
// dispatcher uses this value to gate retry sleep, so a regression
// could stall a client thread for unbounded time.
func TestClampRetryAfterShapes(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{-1, 0},
		{-3600, 0},
		{0, 0},
		{1, 1},
		{299, 299},
		{maxRetryAfterSeconds, maxRetryAfterSeconds},
		{maxRetryAfterSeconds + 1, maxRetryAfterSeconds},
		{99999, maxRetryAfterSeconds},
	}
	for _, tc := range cases {
		if got := clampRetryAfter(tc.in); got != tc.want {
			t.Errorf("clampRetryAfter(%d)=%d; want %d", tc.in, got, tc.want)
		}
	}
}
