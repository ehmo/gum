package toon_test

import (
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestDecodeKeyValueSkipsBlankAndMissingEqualsLines pins decodeKeyValue's
// two continue arms (encoder.go:601-602 blank line, 605-606 no '='),
// exercised via the public Decode entry point. The first non-blank
// line carries '=' so Decode dispatches into decodeKeyValue, then
// passes the full line slice — which includes one blank line and one
// junk line with no '='. Both MUST be silently skipped without losing
// the surrounding key/value pairs.
func TestDecodeKeyValueSkipsBlankAndMissingEqualsLines(t *testing.T) {
	t.Parallel()
	input := []byte("a=1\n\njunk line with no equals\nb=2")
	got, err := toon.Decode(input)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("Decode returned %T; want map[string]any", got)
	}
	if _, hasA := m["a"]; !hasA {
		t.Errorf("decoded map missing key 'a': %v", m)
	}
	if _, hasB := m["b"]; !hasB {
		t.Errorf("decoded map missing key 'b': %v", m)
	}
}
