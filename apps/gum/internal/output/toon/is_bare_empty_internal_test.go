package toon

import "testing"

// TestIsBareEmptyStringRejected pins isBare's `s == "" → return false`
// arm (encoder.go:48-50). Empty strings must be CSV-quoted (so they
// round-trip as `""` rather than vanish), so the bareness predicate
// MUST reject them up front before the keyword/regex checks.
func TestIsBareEmptyStringRejected(t *testing.T) {
	t.Parallel()
	if isBare("") {
		t.Errorf("isBare(\"\") = true; want false (empty needs CSV quoting)")
	}
}
