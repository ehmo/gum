package mcp

import (
	"strings"
	"testing"
)

// TestParseTemplateParamShapes pins the §8.2 safe-served-ref shape
// validation. The four reject branches (prefix mismatch / empty tail /
// over-length tail / path-separator tail) plus the accept branch must
// stay distinguishable so a hostile client can't sneak a path-traversal
// segment past the URI template handler.
func TestParseTemplateParamShapes(t *testing.T) {
	cases := []struct {
		name      string
		uri       string
		prefix    string
		want      string
		wantMatch bool
	}{
		{"happy_path", "gum://results/abc123", "gum://results/", "abc123", true},
		{"prefix_mismatch", "https://results/abc", "gum://results/", "", false},
		{"empty_tail", "gum://results/", "gum://results/", "", false},
		{"path_separator_rejected", "gum://results/a/b", "gum://results/", "", false},
		{"query_rejected", "gum://results/a?x=1", "gum://results/", "", false},
		{"fragment_rejected", "gum://results/a#frag", "gum://results/", "", false},
		{"over_length", "gum://results/" + strings.Repeat("a", 257), "gum://results/", "", false},
		{"exactly_256", "gum://results/" + strings.Repeat("a", 256), "gum://results/", strings.Repeat("a", 256), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTemplateParam(tc.uri, tc.prefix)
			if ok != tc.wantMatch {
				t.Errorf("ok=%v; want %v (got=%q)", ok, tc.wantMatch, got)
			}
			if got != tc.want {
				t.Errorf("got=%q; want %q", got, tc.want)
			}
		})
	}
}
