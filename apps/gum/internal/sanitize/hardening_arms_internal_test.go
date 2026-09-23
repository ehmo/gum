package sanitize

import "testing"

// TestIsASCIIAlnumOrSpaceArms pins the predicate rule 8 decides on. The empty
// arm is not reachable through Sanitize today, because no single codepoint has
// an empty NFKD decomposition, and it is the arm that matters most: an empty
// decomposition read as "all ASCII" would report a violation against a
// character that has no ASCII spelling to suggest.
func TestIsASCIIAlnumOrSpaceArms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"lower", "ignore", true},
		{"upper", "IGNORE", true},
		{"digits", "1234", true},
		{"space", "no 1", true},
		{"combining mark", "é", false},
		{"ascii punctuation", "...", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isASCIIAlnumOrSpace(tc.in); got != tc.want {
				t.Errorf("isASCIIAlnumOrSpace(%q)=%v; want %v", tc.in, got, tc.want)
			}
		})
	}
}
