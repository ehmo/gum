package profile

import "testing"

// TestTruncateStringShapes pins the observable outcomes: zero or negative
// limit is a no-op; len(runes) ≤ limit is a no-op; over-limit clamps to limit
// runes INCLUDING the "…"; multi-byte runes count as one regardless of byte
// width so the ellipsis lands on a rune boundary; and the second return says
// whether the caller must write the <field>_truncated sibling.
func TestTruncateStringShapes(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		limit   int
		want    string
		wantCut bool
	}{
		{"zero_limit_noop", "abcdef", 0, "abcdef", false},
		{"negative_limit_noop", "abcdef", -1, "abcdef", false},
		{"under_limit_noop", "abc", 5, "abc", false},
		{"exact_limit_noop", "abcde", 5, "abcde", false},
		{"over_limit_appends_ellipsis", "abcdef", 3, "ab…", true},
		{"limit_one_is_just_the_ellipsis", "abcdef", 1, "…", true},
		{"multibyte_counts_as_runes", "héllo", 2, "h…", true},
		{"emoji_counts_as_runes", "🐙🐙🐙🐙", 2, "🐙…", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, cut := truncateString(tc.in, tc.limit)
			if got != tc.want {
				t.Errorf("got=%q; want %q", got, tc.want)
			}
			if cut != tc.wantCut {
				t.Errorf("truncated=%v; want %v", cut, tc.wantCut)
			}
			if n := len([]rune(got)); tc.limit > 0 && n > tc.limit {
				t.Errorf("got %d runes; want at most the declared limit %d", n, tc.limit)
			}
		})
	}
}

// TestIsLossyBranches pins every dimension of the lossy detection.
// downstream fixture-comparison logic short-circuits on this so a
// missed branch would silently allow a "lossless" claim for a profile
// that actually drops data.
//
// The collapse argument is the cap in force for the call, which is why the
// last two cases pair a profile with a cap the caller replaced.
func TestIsLossyBranches(t *testing.T) {
	cases := []struct {
		name     string
		p        *Profile
		collapse *CollapseArraysSpec
		want     bool
	}{
		{"nil_profile", nil, nil, false},
		{"empty_profile_lossless", &Profile{}, nil, false},
		{"with_projection", &Profile{Projection: []string{"id"}}, nil, true},
		{"with_keep_fields", &Profile{KeepFields: []string{"id"}}, nil, true},
		{"with_drop_fields", &Profile{DropFields: []string{"id"}}, nil, true},
		{"strip_nulls", &Profile{StripNulls: true}, nil, true},
		{"collapse_arrays", &Profile{CollapseArrays: &CollapseArraysSpec{}}, &CollapseArraysSpec{}, true},
		{"truncate_strings", &Profile{TruncateStrings: &TruncateStringsSpec{}}, nil, true},
		{"dedupe", &Profile{Dedupe: &DedupeSpec{}}, nil, true},
		{"limit_set", &Profile{Limit: 1}, nil, true},
		{"limit_zero_lossless", &Profile{Limit: 0}, nil, false},
		{"caller_cap_on_lossless_profile", &Profile{}, &CollapseArraysSpec{MaxItems: 5}, true},
		{"caller_removed_the_profile_cap", &Profile{CollapseArrays: &CollapseArraysSpec{MaxItems: 5}}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLossy(tc.p, tc.collapse); got != tc.want {
				t.Errorf("got=%v; want %v", got, tc.want)
			}
		})
	}
}
