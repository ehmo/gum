// matrixv0_test.go — a gate row states today's requirement, not v0.1's.
//
// docs/test-matrix.md is the list of gates the current build enforces. Its
// third column names the phase that enforces each row (`v0.1 CI`, `Release`,
// `Same PR`), so a v0.x token there is the column's own vocabulary. In the
// requirement column it is a different claim: "absent from the v0.1.0
// `initialize` response" and "the only v0.1 scope grammar" date a live
// requirement to a release that shipped and passed, leaving a reader at v2.x
// unable to tell whether the row still binds. Thirteen rows carried that
// framing after internal/lint/v0framing_test.go cleared the rest of the tree;
// the noun-modifier spelling ("the v0.1.0 dispatcher") slips past every
// pattern in that gate, because widening those to catch it would flag the
// dependency floors and the historical roadmap sentence it deliberately
// allows.
//
// This gate is scoped to the one file where the distinction is column-shaped,
// so it needs no such exemptions. The dependency floor is the single legitimate
// shape and is recognised by its own spelling: `@v0.8.1` or `v0.13.0+`.
package lint_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// matrixV0Token matches a v0.x version mention in a matrix requirement.
var matrixV0Token = regexp.MustCompile(`v0\.\d+(?:\.\d+)?`)

// matrixV0Floor matches the two dependency-floor spellings, the only shape a
// requirement may legitimately use: `staticcheck@v0.8.1` pins a tool version
// and `v0.13.0+` states a module floor. Both are facts about a dependency, not
// about which gum release the row describes.
var matrixV0Floor = regexp.MustCompile(`@v0\.\d|v0\.\d+(?:\.\d+)?\+`)

// TestMatrixRequirementsNameNoV0Release fails when the requirement column of
// docs/test-matrix.md dates a gate to a v0.x release. The fix is the present
// tense: drop the version and let the sentence describe the build.
func TestMatrixRequirementsNameNoV0Release(t *testing.T) {
	root := repoRootForCites(t)
	path := filepath.Join(root, "docs", "test-matrix.md")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var offenses []string
	for i, line := range strings.Split(string(data), "\n") {
		for _, bad := range matrixV0Offenses(line) {
			offenses = append(offenses, fmt.Sprintf("docs/test-matrix.md:%d: %s", i+1, bad))
		}
	}

	if len(offenses) > 0 {
		t.Errorf("%d matrix requirement(s) date a gate to a v0.x release:\n  %s\n\n"+
			"State what the build does. The phase column keeps its `v0.1 CI` value; "+
			"a dependency floor keeps its `@v0.8.1` or `v0.13.0+` spelling.",
			len(offenses), strings.Join(offenses, "\n  "))
	}
}

// TestMatrixV0OffensesCatchesEachSpelling arms the gate above. A scan that
// passes on a clean file proves nothing about its detector.
func TestMatrixV0OffensesCatchesEachSpelling(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"noun modifier", "| absent from the v0.1.0 `initialize` response | `TestX` | v0.1 CI |", true},
		{"bare adjective", "| the only v0.1 scope grammar | `TestX` | v0.1 CI |", true},
		{"subject", "| v0.1.0 manifest contains thirteen topics | `TestX` | Release |", true},
		{"future target", "| this enables the v0.2.0 enablement to land | `TestX` | v0.1 CI |", true},
		{"phase column only", "| prompts report empty arguments | `TestX` | v0.1 CI |", false},
		{"module floor", "| the pinned floor is v0.13.0+ (Appendix A) | `TestX` | Release |", false},
		{"tool pin", "| CI runs `staticcheck@v0.8.1` | `TestX` | Release |", false},
		{"prose line outside the table", "`v0.1 CI` means the gate is enforced from the first v0.1 commit.", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := len(matrixV0Offenses(tc.line)) > 0
			if got != tc.want {
				t.Errorf("matrixV0Offenses(%q) offending = %v; want %v", tc.line, got, tc.want)
			}
		})
	}
}

// matrixV0Offenses returns the offending fragments in one line of the matrix.
// A line that is not a table row carries no requirement column and is skipped,
// which is what keeps the phase-column legend in the file header legal.
func matrixV0Offenses(line string) []string {
	if !strings.HasPrefix(line, "|") {
		return nil
	}

	cols := strings.Split(line, "|")
	if len(cols) < 3 {
		return nil
	}

	req := cols[1]

	var out []string
	for _, loc := range matrixV0Token.FindAllStringIndex(req, -1) {
		window := req[max(0, loc[0]-1):min(len(req), loc[1]+1)]
		if matrixV0Floor.MatchString(window) {
			continue
		}
		out = append(out, strings.TrimSpace(req[max(0, loc[0]-40):min(len(req), loc[1]+40)]))
	}

	return out
}
