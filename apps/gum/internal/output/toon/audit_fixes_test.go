package toon_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestRoundTripKeyWithEquals pins the audit fix: a map key containing '=' (e.g.
// an OAuth scope or URL used as a key) must survive an encode→decode round trip.
// The decoder previously split on the first '=', so a quoted key like "a=b"
// decoded to the wrong key "a.
func TestRoundTripKeyWithEquals(t *testing.T) {
	in := map[string]any{"https://x/auth=scope": "v", "plain": "p"}
	enc, err := toon.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	dec, err := toon.Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v (encoded: %q)", err, enc)
	}
	got, ok := dec.(map[string]any)
	if !ok {
		t.Fatalf("decoded type %T, want map", dec)
	}
	if got["https://x/auth=scope"] != "v" {
		t.Errorf("key containing '=' lost in round trip: %#v (encoded: %q)", got, enc)
	}
	if got["plain"] != "p" {
		t.Errorf("plain key corrupted: %#v", got)
	}
}

// TestEncodeHugeWholeFloatNotClamped pins the audit fix: a whole-number float64
// beyond int64 range must not silently clamp to MaxInt64 — it renders with full
// digits instead.
func TestEncodeHugeWholeFloatNotClamped(t *testing.T) {
	enc, err := toon.Encode(map[string]any{"size": float64(1e19)})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	s := string(enc)
	if strings.Contains(s, "9223372036854775807") {
		t.Errorf("huge whole float clamped to MaxInt64: %q", s)
	}
	if !strings.Contains(s, "10000000000000000000") {
		t.Errorf("huge whole float not rendered with full digits: %q", s)
	}
}
