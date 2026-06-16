package toon_test

import (
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestEncodeMapWithNilValueTrips IsSentinelEmpty pins isSentinelEmpty's
// `v == nil → return true` arm (encoder.go:167-169). Reached by encoding
// a map whose sole value is a literal nil — allSentinelEmpty walks the
// map and asks isSentinelEmpty about the nil entry, so encodeMap must
// emit the `{}` sentinel form rather than a key/value row.
func TestEncodeMapWithNilValueEmitsSentinel(t *testing.T) {
	t.Parallel()
	got, err := toon.Encode(map[string]any{"k": nil})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(got) != "{}\n" {
		t.Errorf("got=%q; want %q", string(got), "{}\n")
	}
}
