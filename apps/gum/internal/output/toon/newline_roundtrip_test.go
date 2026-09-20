package toon_test

import (
	"reflect"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// A string value that contains a newline is ordinary API data: a Gmail snippet,
// a Docs paragraph, a Calendar description. The encoder CSV-quotes it, so the
// quoted value spans two physical lines. Decode used to split the document on
// every "\n" without tracking quotes, which fabricated a field when the tail of
// the value happened to contain "=", and truncated the value otherwise. Both
// are silent: the caller gets a map with wrong contents and no error.
func TestDecodeKeyValueKeepsNewlineInValue(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
	}{
		{"tail looks like a key", map[string]any{"a": "x\ny=z", "b": float64(1)}},
		{"plain two-line value", map[string]any{"a": "line1\nline2"}},
		{"trailing newline", map[string]any{"note": "a\nb"}},
		{"value with comma and newline", map[string]any{"body": "one,two\nthree,four"}},
		{"quoted key and newline value", map[string]any{"a=b": "p\nq"}},
		{"three physical lines", map[string]any{"k": "1\n2\n3", "z": "tail"}},
		{"embedded quote and newline", map[string]any{"k": "say \"hi\"\nbye"}},
		{"value ends with newline", map[string]any{"k": "v\n"}},
		{"value is only a newline", map[string]any{"k": "\n"}},
		{"blank line inside value", map[string]any{"k": "a\n\nb"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := toon.Encode(tc.in)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			got, err := toon.Decode(enc)
			if err != nil {
				t.Fatalf("Decode(%q): %v", enc, err)
			}
			if !reflect.DeepEqual(got, tc.in) {
				t.Errorf("round-trip lost data\n  encoded %q\n  want    %#v\n  got     %#v", enc, tc.in, got)
			}
		})
	}
}

// A bare scalar string holding a newline used to reach the CSV-table branch,
// because the first physical line carried neither "=" nor "," and the document
// had more than one physical line. The table parser then returned an empty
// slice: the value and its type were both gone.
func TestDecodeScalarKeepsNewline(t *testing.T) {
	for _, in := range []string{
		"scalar\nwith newline",
		"a\nb\nc",
		"only trailing\n",
	} {
		enc, err := toon.Encode(in)
		if err != nil {
			t.Fatalf("Encode(%q): %v", in, err)
		}
		got, err := toon.Decode(enc)
		if err != nil {
			t.Fatalf("Decode(%q): %v", enc, err)
		}
		if got != in {
			t.Errorf("round-trip lost data\n  encoded %q\n  want    %q\n  got     %#v", enc, in, got)
		}
	}
}
