package risor

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

// TestTruncateToUTF8BoundaryShapes pins the three control branches —
// non-positive cap, no-op (already short enough), and walk-back-to-
// rune-start — plus the worst-case where a multi-byte rune straddles
// the cap. The output must always be valid UTF-8 so downstream
// JSON-marshal or template-render paths never see a half-rune.
func TestTruncateToUTF8BoundaryShapes(t *testing.T) {
	cases := []struct {
		name    string
		in      []byte
		max     int
		want    []byte
		wantLen int
	}{
		{"zero_max", []byte("abc"), 0, []byte{}, 0},
		{"negative_max", []byte("abc"), -1, []byte{}, 0},
		{"already_short", []byte("ab"), 5, []byte("ab"), 2},
		{"ascii_exact", []byte("abcd"), 4, []byte("abcd"), 4},
		{"ascii_truncate", []byte("abcde"), 3, []byte("abc"), 3},
		// "héllo" — h(1) é(2bytes 0xc3 0xa9) l(1) l(1) o(1) = 7 bytes
		// total. max=2 splits é (b[2]=0xa9 is a continuation byte);
		// walk-back lands at i=1, returning just "h".
		{"multibyte_walks_back", []byte("héllo"), 2, []byte("h"), 1},
		// max=3 happens to land at b[3]='l' which IS a rune start, so
		// no walk-back is needed and the result keeps the full é.
		{"multibyte_lands_on_boundary", []byte("héllo"), 3, []byte("hé"), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateToUTF8Boundary(tc.in, tc.max)
			if !bytes.Equal(got, tc.want) {
				t.Errorf("got=%q; want %q", got, tc.want)
			}
			if len(got) != tc.wantLen {
				t.Errorf("len=%d; want %d", len(got), tc.wantLen)
			}
			if !utf8.Valid(got) {
				t.Errorf("output not valid UTF-8: %q", got)
			}
		})
	}
}
