package mcp

import "testing"

// TestClampIntParseFailureReturnsDefault pins clampInt's
// `strconv.Atoi err → return def` arm (meta_tool_profiles.go:57-61).
// A non-numeric raw must yield the default and emit a warn log.
func TestClampIntParseFailureReturnsDefault(t *testing.T) {
	t.Parallel()
	if got := clampInt("k", "not-a-number", 7, 1, 10); got != 7 {
		t.Errorf("got=%d; want 7 (default)", got)
	}
}

// TestClampIntBelowRangeClampsUp pins the `v < lo → return lo` arm
// (meta_tool_profiles.go:62-66). 0 below a lo of 5 must clamp to 5.
func TestClampIntBelowRangeClampsUp(t *testing.T) {
	t.Parallel()
	if got := clampInt("k", "0", 7, 5, 10); got != 5 {
		t.Errorf("got=%d; want 5 (lo)", got)
	}
}

// TestClampIntAboveRangeClampsDown pins the `v > hi → return hi` arm
// (meta_tool_profiles.go:67-71). 99 above a hi of 10 must clamp to 10.
func TestClampIntAboveRangeClampsDown(t *testing.T) {
	t.Parallel()
	if got := clampInt("k", "99", 7, 1, 10); got != 10 {
		t.Errorf("got=%d; want 10 (hi)", got)
	}
}

// TestClampIntInRangeReturnsParsed pins the fall-through `return v`
// arm (meta_tool_profiles.go:72). A value inside [lo, hi] is returned
// unchanged.
func TestClampIntInRangeReturnsParsed(t *testing.T) {
	t.Parallel()
	if got := clampInt("k", "5", 7, 1, 10); got != 5 {
		t.Errorf("got=%d; want 5 (parsed)", got)
	}
}
