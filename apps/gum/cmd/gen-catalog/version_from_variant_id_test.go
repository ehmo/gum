package main

import "testing"

// TestVersionFromVariantID pins every observable arm of the inline
// `.v<digits>.` extractor used to populate variant Version fields:
//
//   - "gmail.v1.rest…"   → "v1" (single-digit, mid-string)
//   - "calendar.v3.rest" → "v3"
//   - "x.v12.y"          → "v12" (multi-digit)
//   - "no-version-here"  → "" (fall-through)
//   - "" / "..v..."      → "" (fall-through; not enough chars OR letter after v)
//
// Without the fall-through case pinned, a regression that made the
// extractor return a stub like "v0" on no-match would silently mis-
// populate every catalog row that didn't carry an embedded version
// token.
func TestVersionFromVariantID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"gmail.v1.rest.users.messages.list", "v1"},
		{"calendar.v3.rest.events.list", "v3"},
		{"some.v12.path", "v12"},
		{"no.version.here", ""},
		{"", ""},
		{"x", ""},
		{".vX.y", ""}, // letter after v → no match
	}
	for _, tc := range cases {
		if got := versionFromVariantID(tc.in); got != tc.want {
			t.Errorf("versionFromVariantID(%q)=%q; want %q", tc.in, got, tc.want)
		}
	}
}
