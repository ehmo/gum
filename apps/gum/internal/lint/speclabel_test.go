// speclabel_test.go — a quoted section label must exist in the document.
//
// internal/lint/speccite_test.go holds the tree at section numbers because
// they survive edits. The quoted label beside the number does not: renaming a
// heading leaves every citation of its old name intact and unchecked.
// docs/test-matrix.md cited spec §7 "v0.1.0 login surface" after the spec
// renamed that paragraph to "Login surface (normative; transitional)", so the
// row pointed a reader at a phrase the spec no longer contained anywhere.
//
// This gate resolves each quoted label against the normative documents. It
// matches the label only where it sits directly beside the section number,
// because that is the position that makes it a citation; a quoted error code
// later in the same sentence is not one.
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

// specLabelCite matches a section number followed by its quoted label. The
// optional comma covers the "§9.2, \"Savings-claim scope\"" spelling.
var specLabelCite = regexp.MustCompile(`§\d+(?:\.\d+)*,?\s+"([^"]{3,80})"`)

// specLabelMore matches each additional label in a run, as in
// `§7 "Login surface", "Just-in-time authorization"`.
var specLabelMore = regexp.MustCompile(`^,\s+"([^"]{3,80})"`)

// specLabelDocs are the documents a quoted label may resolve against: the
// product contract and its normative supporting contracts. A label is checked
// against the union rather than against the one document the sentence names,
// which keeps the gate from parsing prose to decide whose §8.7 is meant.
var specLabelDocs = []string{
	"docs/spec.md",
	"docs/catalog-abi.md",
	"docs/plugin-contract.md",
	"docs/expression-profile-dsl.md",
}

// TestSpecLabelCitationsResolve fails when a citation quotes a section label
// that no normative document contains. The fix is to quote the heading as it
// reads now, or to drop the quotation and keep the section number.
func TestSpecLabelCitationsResolve(t *testing.T) {
	root := repoRootForCites(t)
	self := specLabelSelfPath(t)

	corpus, complete := specLabelCorpus(t, root)
	if !complete {
		t.Skip("this tree does not ship every normative document")
	}

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

		for i, line := range strings.Split(string(data), "\n") {
			for _, label := range specLabelsIn(line) {
				if strings.Contains(corpus, label) {
					continue
				}
				offenses = append(offenses, fmt.Sprintf("%s:%d: %q", relPath(root, path), i+1, label))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(offenses) > 0 {
		t.Errorf("%d citation(s) quote a section label no normative document contains:\n  %s\n\nchecked against: %s",
			len(offenses), strings.Join(offenses, "\n  "), strings.Join(specLabelDocs, ", "))
	}
}

// TestSpecLabelsInCatchesEachShape arms the gate above.
func TestSpecLabelsInCatchesEachShape(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []string
	}{
		{"plain", `spec §7 "BYO grant storage" binds the token`, []string{"BYO grant storage"}},
		{"comma before label", `§9.2, "Savings-claim scope"`, []string{"Savings-claim scope"}},
		{"two labels", `spec §7 "Login surface", "Just-in-time authorization"`,
			[]string{"Login surface", "Just-in-time authorization"}},
		{"backticked label", "§13.1 \"Tolerant `logging/setLevel` handling\"", []string{"Tolerant `logging/setLevel` handling"}},
		{"error code is not a label", `(§5.1), the call returns {"error_code": "OP_NOT_FOUND"}`, nil},
		{"section alone", `see spec §6.1.2 for the binding tuple`, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := specLabelsIn(tc.line)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("specLabelsIn(%q) = %v; want %v", tc.line, got, tc.want)
			}
		})
	}
}

// specLabelsIn returns every quoted section label cited on one line.
func specLabelsIn(line string) []string {
	var out []string

	for _, loc := range specLabelCite.FindAllStringSubmatchIndex(line, -1) {
		out = append(out, line[loc[2]:loc[3]])

		rest := line[loc[1]:]
		for {
			more := specLabelMore.FindStringSubmatchIndex(rest)
			if more == nil {
				break
			}
			out = append(out, rest[more[2]:more[3]])
			rest = rest[more[1]:]
		}
	}

	return out
}

// specLabelCorpus returns the concatenated normative documents and whether the
// tree ships all of them. A label may sit in any of the four, so a partial
// corpus cannot decide: the public export withholds the spec, and judging its
// Go comments against the other three reported 36 live citations as dead.
func specLabelCorpus(t *testing.T, root string) (string, bool) {
	t.Helper()

	var b strings.Builder

	for _, rel := range specLabelDocs {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", false
		}
		b.Write(data)
		b.WriteString("\n")
	}

	return b.String(), true
}

// specLabelSelfPath returns this file's own path so the scan skips it. The
// synthetic lines in the detector table are citations by the gate's own
// definition.
func specLabelSelfPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return file
}
