// matrixrow_test.go — cite a test-matrix row by its test, not by its ordinal.
//
// docs/test-matrix.md is a table with no stable row identifier. A citation
// that names an ordinal ("docs/test-matrix.md row 151") resolves only until
// the next row lands above it, and rows land constantly: a 2026-09 sweep of
// 71 such citations found 65 of them pointing at an unrelated requirement,
// most off by five to ten, and one naming a row past the end of the table.
// That is the same failure internal/lint/speccite_test.go exists to prevent
// for spec line numbers.
//
// The matrix already carries a stable anchor: its `Proof artifact` column
// names the tests. A citation that names the test, or quotes the requirement,
// survives every insertion, and a reader can find the row with one grep.
//
// Scope: the gate reads a row number only where the word "matrix" sits within
// a few characters of it, which is what makes the number a citation. A bare
// "row 2" elsewhere is a CSV or spreadsheet row and is left alone.
package lint_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// matrixRowCite matches a matrix reference followed by a row ordinal. The
// gap allows the punctuation and comment marker that a wrapped Go comment
// puts between them, as in "(docs/test-matrix.md\n// row 215, bead …)", and
// the clause an assertion message puts between them ("fuzz matrix has %d
// entries; row 209 pins six targets").
var matrixRowCite = regexp.MustCompile(`(?i)matrix(?:\.md)?\b[^\n]{0,30}\brows? \d+`)

// matrixCommentMarker collapses the comment prefix of a continuation line so
// a citation split across two lines reads as one string.
var matrixCommentMarker = regexp.MustCompile(`\s*//\s*`)

// TestNoCitationNamesAMatrixRowNumber fails when prose cites a test-matrix
// row by ordinal. The fix is to name the test the row's proof column lists,
// or to quote the requirement, and drop the number.
func TestNoCitationNamesAMatrixRowNumber(t *testing.T) {
	root := repoRootForCites(t)
	self := matrixRowSelfPath(t)

	var offenses []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipCiteDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if path == self || skipCiteFile(d.Name()) || (ext != ".md" && ext != ".go") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !utf8.Valid(data) {
			return nil
		}

		for _, cite := range matrixRowCitesIn(string(data)) {
			offenses = append(offenses, fmt.Sprintf("%s:%d: %s", relPath(root, path), cite.line, cite.text))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(offenses) > 0 {
		t.Errorf("%d citation(s) name a test-matrix row by ordinal:\n  %s\n\ncite the test the row's proof column names instead; ordinals move whenever a row lands above them",
			len(offenses), strings.Join(offenses, "\n  "))
	}
}

// TestMatrixRowCitesCatchesEachSpelling arms the gate above. A clean tree
// proves nothing about the detector, so each spelling the sweep found gets a
// synthetic line here, next to the row references that are not citations.
func TestMatrixRowCitesCatchesEachSpelling(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"bare matrix", "// Matrix row 209. The release gates live in manifests.", 1},
		{"path and ordinal", "// TestStdioFramingClean is the docs/test-matrix.md row 193 proof.", 1},
		{"unqualified path", "// (spec.md §6.1 \"Destructive budget\"; test-matrix.md row 57).", 1},
		{"plural run", "// beads gum-7oap, gum-z8u9; docs/test-matrix.md rows 64 and 151).", 1},
		{"hyphenated, no path", "// test-matrix row 5 carves these out as exempt.", 1},
		{"split across comment lines", "// Spec §5.2 overrides gate (docs/test-matrix.md\n// row 215, bead gum-lpra).", 1},
		{"error message", "t.Fatalf(\"fuzz matrix has %d entries; row 209 pins six targets\", n)", 1},
		{"named test instead", "// TestStdioFramingClean is the docs/test-matrix.md proof.", 0},
		{"doc without an ordinal", "// docs/test-matrix.md names this test.", 0},
		{"spreadsheet row", "| `Sheet1!1:1` | entire row 1 |", 0},
		{"csv fixture row", "// - row 0: id=msg001, snippet=null", 0},
		{"toon decode error", "t.Fatal(\"Encode(homog CSV with chan in row 2)=nil err\")", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matrixRowCitesIn(tc.body)
			if len(got) != tc.want {
				t.Errorf("matrixRowCitesIn(%q) found %d; want %d", tc.body, len(got), tc.want)
			}
		})
	}
}

// matrixRowCitation is one offending citation: the line it starts on and the
// matched text.
type matrixRowCitation struct {
	line int
	text string
}

// matrixRowCitesIn returns every ordinal citation in body. Each line is
// tested joined with the line after it, so a citation a comment wrapped
// across two lines is still one match, reported on the line it starts on.
func matrixRowCitesIn(body string) []matrixRowCitation {
	lines := strings.Split(body, "\n")

	var out []matrixRowCitation
	for i, line := range lines {
		window := line
		if i+1 < len(lines) {
			window = line + " " + matrixCommentMarker.ReplaceAllString(lines[i+1], " ")
		}

		loc := matrixRowCite.FindStringIndex(window)
		if loc == nil {
			continue
		}

		// A match that lies wholly in the joined line belongs to that line,
		// which reports it on its own pass.
		if loc[0] > len(line) {
			continue
		}

		out = append(out, matrixRowCitation{line: i + 1, text: strings.TrimSpace(window[loc[0]:loc[1]])})
	}

	return out
}

// matrixRowSelfPath returns this file's own path so the walk skips it. The
// detector table above is a page of citations by the gate's own definition.
func matrixRowSelfPath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return file
}
