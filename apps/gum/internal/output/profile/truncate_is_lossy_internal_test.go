package profile

import "testing"

// TestTruncateStringShapes pins the four observable outcomes: zero or
// negative limit is a no-op; len(runes) ≤ limit is a no-op; over-limit
// truncates to N runes + "…"; multi-byte runes count as one regardless
// of byte width so the ellipsis lands on a rune boundary.
func TestTruncateStringShapes(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		limit int
		want  string
	}{
		{"zero_limit_noop", "abcdef", 0, "abcdef"},
		{"negative_limit_noop", "abcdef", -1, "abcdef"},
		{"under_limit_noop", "abc", 5, "abc"},
		{"exact_limit_noop", "abcde", 5, "abcde"},
		{"over_limit_appends_ellipsis", "abcdef", 3, "abc…"},
		{"multibyte_counts_as_runes", "héllo", 2, "hé…"},
		{"emoji_counts_as_runes", "🐙🐙🐙🐙", 2, "🐙🐙…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncateString(tc.in, tc.limit); got != tc.want {
				t.Errorf("got=%q; want %q", got, tc.want)
			}
		})
	}
}

// TestIsLossyBranches pins every dimension of the lossy detection.
// downstream fixture-comparison logic short-circuits on this so a
// missed branch would silently allow a "lossless" claim for a profile
// that actually drops data.
func TestIsLossyBranches(t *testing.T) {
	cases := []struct {
		name string
		p    *Profile
		want bool
	}{
		{"nil_profile", nil, false},
		{"empty_profile_lossless", &Profile{}, false},
		{"with_projection", &Profile{Projection: []string{"id"}}, true},
		{"with_keep_fields", &Profile{KeepFields: []string{"id"}}, true},
		{"with_drop_fields", &Profile{DropFields: []string{"id"}}, true},
		{"strip_nulls", &Profile{StripNulls: true}, true},
		{"collapse_arrays", &Profile{CollapseArrays: &CollapseArraysSpec{}}, true},
		{"truncate_strings", &Profile{TruncateStrings: &TruncateStringsSpec{}}, true},
		{"dedupe", &Profile{Dedupe: &DedupeSpec{}}, true},
		{"limit_set", &Profile{Limit: 1}, true},
		{"limit_zero_lossless", &Profile{Limit: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLossy(tc.p); got != tc.want {
				t.Errorf("got=%v; want %v", got, tc.want)
			}
		})
	}
}
