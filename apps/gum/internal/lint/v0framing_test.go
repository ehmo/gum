// v0framing_test.go — say what the build does, not which v0.x does it.
//
// gum ships at v2.x. Roughly 294 lines across 80 files still used `v0.1.0` as
// a synonym for "the current build" and promised features "deferred to v0.2.0"
// or "shipping in v0.4.0". Both halves were false: the releases those targets
// named came and went, and the features are either built or they are not.
// docs/spec.md §15 states the governing rule: "The deferred-features table
// names no target version: the v0.x targets it used to carry outlived the
// releases meant to keep them, and a feature is either built or it is not."
//
// The tree still mentions v0.x for good reasons, so this gate does not ban the
// token. Dependency floors (`staticcheck@v0.8.1`, `go-keyring v0.2.6+`), the
// on-disk migration sentinel `v0.1.0->v0.4.0`, the `v0.1 CI` phase column in
// docs/test-matrix.md, ldflags examples and version fixtures in tests all name
// a v0.x release and all state a fact. What this gate matches is the two
// sentence shapes that carry the falsehood:
//
//  1. dating current behavior to a v0.x release — "in v0.1.0", "for v0.1.0",
//     "since v0.1.0", "as of v0.1.0", "v0.1.0 does not create these",
//     "v0.1.0 has no gRPC client", "the v0.1.0 runtime", "v0.1.0-disabled".
//  2. promising a v0.x target — "deferred to v0.3.0", "ships in v0.2.0",
//     "wait for v0.2.0", "rejected before v0.4.0", "v0.2.0 will replace".
//
// Both shapes say something a reader can act on and be wrong about. A bare
// version token in a dependency table does not.
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

// v0Dating matches a v0.x version used to date behavior: the preposition in
// front is what separates "not read in v0.1.0" from a dependency row that
// merely lists `v0.13.0`.
var v0Dating = regexp.MustCompile(`(?i)\b(?:in|for|since|as of)\s+v0\.\d`)

// v0Subject matches a v0.x version used as the sentence's subject, which is
// the same claim with the preposition dropped: "v0.1.0 does not create these",
// "v0.1.0 release binaries import neither". The verb list is deliberately
// short and excludes the copula, because "v0.13.0 is the floor that bundles
// cmpenv" is a dependency fact, not a claim about gum's behavior.
var v0Subject = regexp.MustCompile(`(?i)\bv0\.\d+(?:\.\d+)?(?:\.x)?\s+` +
	`(?:does|do|ships?|supports?|routes?|holds?|requires?|treats?|normalizes?|` +
	`creates?|binaries|catalogs?|runtime|implementation|stubs?|minimal|release)\b`)

// v0Promise matches a feature promised to a future v0.x release. Every one of
// these releases has shipped, so the promise is a statement about the past
// that reads as a commitment.
var v0Promise = regexp.MustCompile(`(?i)\b(?:deferred to|ships? in|lands? in|` +
	`arrives? in|wait for|until|before|replaced in)\s+v0\.\d`)

// v0Will matches the same promise with the version in front: "v0.2.0 will
// replace each stub body".
var v0Will = regexp.MustCompile(`(?i)\bv0\.\d+(?:\.\d+)?\s+will\b`)

// v0Hyphen matches the compound spellings that skip the verb entirely:
// "the v0.1.0-disabled gum_oauth strategy", "v0.1-only behavior".
var v0Hyphen = regexp.MustCompile(`(?i)\bv0\.\d+(?:\.\d+)?-(?:disabled|only|era)\b`)

// v0FramingExts are the file kinds scanned. Go source and Markdown hold the
// comments and docs; JSON holds the embedded manifest and schema `description`
// strings, which are prose served to readers the same way.
var v0FramingExts = map[string]bool{
	".go":   true,
	".md":   true,
	".json": true,
}

// v0FramingSkipFiles is the exemption for point-in-time records on top of the
// set skipCiteFile already covers. docs/PROCESS.md narrates what each past
// spec revision decided, including targets later superseded; rewriting those
// sentences would falsify the record.
var v0FramingSkipFiles = map[string]bool{
	"PROCESS.md": true,
}

// TestNoV0FramingForCurrentBehavior walks the repository and fails on any
// sentence that dates current behavior to a v0.x release or promises a v0.x
// target. The fix is to state what the build does: "not built", "not wired",
// "only risor is supported", or the plain present tense.
func TestNoV0FramingForCurrentBehavior(t *testing.T) {
	root := repoRootForCites(t)
	self := v0FramingSelfPath(t)

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
		if path == self || skipV0FramingFile(d.Name()) || !v0FramingExts[filepath.Ext(path)] {
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
			for _, bad := range v0FramingOffenses(line) {
				offenses = append(offenses, fmt.Sprintf("%s:%d: %s", relPath(root, path), i+1, bad))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(offenses) > 0 {
		t.Errorf("%d line(s) date current behavior to a v0.x release or promise a v0.x target. "+
			"State what the build does instead:\n%s",
			len(offenses), strings.Join(offenses, "\n"))
	}
}

// TestV0FramingOffensesCatchesEachDisguise pins the matcher. The walk above
// only proves the tree is clean today; a broken regex would keep passing while
// the framing came back. The legal cases matter as much as the offenses: this
// gate is useless if it forces a dependency table or a version fixture to lie.
//
// Each case pins whether the line is flagged, not how many fragments come
// back: the patterns overlap on purpose, so "ship in v0.2.0" matches both the
// promise shape and the dating shape and reports two fragments.
func TestV0FramingOffensesCatchesEachDisguise(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		flagged bool
	}{
		{"dating with in", "// usage.jsonl is not read in v0.1.0.", true},
		{"dating with for", "// Vendor mode: OFF for v0.1.0.", true},
		{"dating with since", "// The tool warns since v0.1.0 doesn't create them.", true},
		{"dating with as of", "// No project-local equivalent as of v0.1.0.", true},
		{"version as subject", `"found %d sub-buckets; v0.1.0 does not create these"`, true},
		{"version as subject plural", "// v0.1.0 binaries import no OpenTelemetry package.", true},
		{"deferred to", "// A sidecar BoltDB index is deferred to v0.3.0.", true},
		{"ships in", "// The subprocess binaries ship in v0.2.0.", true},
		{"wait for", "// so the caller can wait for v0.2.0 gRPC support.", true},
		{"rejected before", "// service_root_template is rejected before v0.4.0.", true},
		{"version will", "// v0.2.0 will replace each stub body with a typed call.", true},
		{"hyphen disabled", "// pins that no variant uses the v0.1.0-disabled strategy.", true},
		{"dependency floor", "// go install honnef.co/go/tools/cmd/staticcheck@v0.8.1", false},
		{"dependency copula", "// v0.13.0 is the floor that bundles the cmpenv comparator.", false},
		{"dependency row", "| `github.com/tiktoken-go/tokenizer` | v0.1.1 | cl100k_base |", false},
		{"phase column", "| requirement | `TestCatalogIntegrity` | v0.1 CI |", false},
		{"migration sentinel", `const SentinelValue = "v0.1.0->v0.4.0"`, false},
		{"ldflags example", "// version is set via -ldflags='-X main.version=v0.1.0'.", false},
		{"version fixture", `dest, err := WriteGUMmd(dir, dir, "v0.1.0-test", true)`, false},
		{"rejected tag example", "tags (e.g. `v0.2.0-rc1`) are rejected by validate-tag", false},
		{"historical roadmap", "// The week-by-week v0.1.0 delivery plan is finished work.", false},
		{"sdk api note", "// CallToolParams.Arguments is `any` in go-sdk v0.2.0.", false},
		{"current version", "// Only risor is supported in v2.2.1.", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := v0FramingOffenses(tc.line)
			if (len(got) > 0) != tc.flagged {
				t.Errorf("v0FramingOffenses(%q) = %v; flagged=%v want %v",
					tc.line, got, len(got) > 0, tc.flagged)
			}
		})
	}
}

// v0FramingOffenses returns the offending fragments on one line. It reports
// each fragment rather than the whole line, so a sentence carrying both a
// dating clause and a promise reports both.
func v0FramingOffenses(line string) []string {
	var out []string

	for _, pat := range []*regexp.Regexp{v0Dating, v0Subject, v0Promise, v0Will, v0Hyphen} {
		out = append(out, pat.FindAllString(line, -1)...)
	}

	return out
}

// skipV0FramingFile reports whether one file is exempt. It layers this gate's
// own exemptions over the point-in-time records skipCiteFile already covers.
func skipV0FramingFile(name string) bool {
	return skipCiteFile(name) || v0FramingSkipFiles[name]
}

// v0FramingSelfPath returns this file's own path so the walk skips it. The
// table above is a list of offenses by the gate's own definition. It cannot
// call the helper in speccite_test.go: runtime.Caller(0) reports the frame of
// the function it sits in, so that helper always names its own file.
func v0FramingSelfPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return file
}
