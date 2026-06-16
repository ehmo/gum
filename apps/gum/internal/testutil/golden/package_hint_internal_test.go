package golden

import (
	"strings"
	"testing"
)

// TestPackageHintBranches pins every arm of the marker-search loop:
//   - A path under /internal/<pkg>/ collapses to "./internal/<pkg>/..."
//   - A path under /cmd/<pkg>/ collapses to "./cmd/<pkg>/..."
//   - A path under neither falls through to "./..." so users still see
//     a runnable hint instead of an empty string.
// A regression that drops the leading "./" or appends the file basename
// instead of dir would break the copy-paste UX in test-failure messages.
func TestPackageHintBranches(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"internal_path", "/Users/x/proj/internal/output/toon/golden_test.go", "./internal/output/toon/..."},
		{"cmd_path", "/Users/x/proj/cmd/gum/testdata/foo.golden", "./cmd/gum/testdata/..."},
		{"neither_marker_falls_back", "/Users/x/proj/docs/whatever.md", "./..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := packageHint(tc.in)
			if got != tc.want {
				t.Errorf("packageHint(%q)=%q; want %q", tc.in, got, tc.want)
			}
			if !strings.HasPrefix(got, "./") {
				t.Errorf("hint %q missing './' prefix", got)
			}
		})
	}
}
