package toon_test

import (
	"reflect"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestEncodeAllEmptyMapKeepsKeys pins gum-wzvh against spec §"Null
// representation (normative)": an empty field is null, a quoted empty field is
// the empty string, and TOON is lossless for that distinction. Collapsing a
// populated map to {} because every value happened to be empty threw away both
// key names with no lossy flag anywhere to report it.
func TestEncodeAllEmptyMapKeepsKeys(t *testing.T) {
	t.Parallel()
	in := map[string]any{"error": "", "status": nil}
	got, err := toon.Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(got) == "{}\n" {
		t.Fatalf("all-empty map collapsed to the empty-object sentinel; both keys were lost")
	}
	back, err := toon.Decode(got)
	if err != nil {
		t.Fatalf("Decode(%q): %v", got, err)
	}
	m, ok := back.(map[string]any)
	if !ok {
		t.Fatalf("Decode returned %T; want map[string]any", back)
	}
	if !reflect.DeepEqual(m, in) {
		t.Errorf("round trip = %#v; want %#v (encoded %q)", m, in, got)
	}
}

// TestEncodeGenuinelyEmptyMapIsSentinel keeps the other half: a map with no
// keys at all still encodes as the {} sentinel.
func TestEncodeGenuinelyEmptyMapIsSentinel(t *testing.T) {
	t.Parallel()
	got, err := toon.Encode(map[string]any{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if string(got) != "{}\n" {
		t.Errorf("got=%q; want %q", string(got), "{}\n")
	}
}
