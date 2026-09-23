// speccite_test.go — spec citations name a section, never a line number.
//
// Line numbers are not an anchor. Every edit above a cited line invalidates
// the citation, and nothing recomputes it: a scan in 2026-09 found 506 line
// citations across the tree, 198 of them pointing outside the section they
// claimed and 65 carrying no section at all. Three samples of what that looks
// like once it has drifted: docs/plugin-author-guide.md cited "spec §8 lines
// 1624-1641" when §8 had moved to 1682-2034, so both numbers landed in §7;
// internal/mcp/plugin_resource.go quoted "spec line 2520 says Same object
// carried inside the", by then the §10.2 heading; internal/mcp/tier_a_abi_test.go
// cited spec line 366, by then blank.
//
// The third disguise is a line number wearing a section sign: §1421, §2129,
// §918. docs/spec.md numbers §1 through §15 and nests no deeper than three
// components (§5.1.1, §10.0.1), so a section sign followed by three or more
// digits is always a line number. A scan in 2026-09 found 392 of those across
// 74 distinct numbers.
//
// Section numbers survive edits because docs/spec.md numbers its own headings.
// This gate holds the tree at that anchor.
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

// specCiteLine matches a "line 235" or "lines 1624-1641" fragment. The left
// context decides whether it is a spec citation; the fragment alone is not
// enough, because a parser error message ("profile: line 1: unknown key") and
// a stack frame both carry the same shape.
var specCiteLine = regexp.MustCompile(`\blines?\s+\d+(?:\s*[-\x{2013}]\s*\d+)?`)

// specCiteColon matches the other spelling of the same mistake, a path with a
// line suffix: docs/spec.md:2379. No left context is needed; the filename is
// already in the match.
var specCiteColon = regexp.MustCompile(`spec\.md:\d+`)

// specCiteSection matches a spec line number wearing a section sign, the
// hardest spelling to spot: §1421 reads like a section until you count the
// digits. The spec's highest section is §15 and its deepest is §10.0.1, so
// three or more leading digits after the sign cannot be a section.
var specCiteSection = regexp.MustCompile(`§\d{3,}`)

// specCiteContext is how far left of a "line NNN" fragment the scan looks for
// evidence that the fragment cites the spec. 90 bytes covers the shapes in the
// tree, from "(spec line 232)" to "Spec §3.1 step 7, line 235".
const specCiteContext = 90

// specCiteSkipDirs are the named trees the gate does not scan. The first two
// are build and tooling output. The last two are point-in-time records: a
// review note and a shipped release note say what their author saw on the day,
// and rewriting a citation inside one would falsify that.
//
// Dot-directories are skipped by skipCiteDir rather than named here, so the
// version-control and issue-tracker stores need no entry.
var specCiteSkipDirs = map[string]bool{
	"node_modules": true,
	"dist":         true,
	"research":     true, // docs/research
	"releases":     true, // docs/releases
}

// skipCiteDir reports whether the walk should skip one directory. The
// dot-directories hold version-control, issue-tracker and editor state. The one
// that holds tracked source, .github, carries only YAML workflow files, which
// neither gate in this package scans.
func skipCiteDir(name string) bool {
	return strings.HasPrefix(name, ".") || specCiteSkipDirs[name]
}

// specCiteSkipFiles is the same exemption for single files. Both are
// point-in-time records: CHANGELOG.md is history, and .scratch-pad.md holds
// session notes that quote wrong citations in order to correct them.
var specCiteSkipFiles = map[string]bool{
	"CHANGELOG.md":    true,
	".scratch-pad.md": true,
}

// specCiteSkipPrefixes exempts published release notes by name. docs/
// release-notes-vX.Y.Z.md shipped with a tag and says what its author saw.
var specCiteSkipPrefixes = []string{"release-notes-"}

// skipCiteFile reports whether one file is exempt from the scan.
func skipCiteFile(name string) bool {
	if specCiteSkipFiles[name] {
		return true
	}
	for _, prefix := range specCiteSkipPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

// specCiteExts are the file kinds scanned. Go source and Markdown are where
// every citation in the tree lives; JSON schemas cite by `$comment` text but
// use section numbers already.
var specCiteExts = map[string]bool{
	".go": true,
	".md": true,
}

// TestSpecCitationsNameASection walks the repository and fails on any spec
// citation that carries a line number. The fix is always the same: keep the
// section reference and drop the line fragment, or, when the citation names no
// section, resolve one from the text it quotes.
func TestSpecCitationsNameASection(t *testing.T) {
	root := repoRootForCites(t)
	self := selfPath(t)

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
		if path == self || skipCiteFile(d.Name()) || !specCiteExts[filepath.Ext(path)] {
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
			for _, bad := range citeOffenses(line) {
				offenses = append(offenses, fmt.Sprintf("%s:%d: %s", relPath(root, path), i+1, bad))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(offenses) > 0 {
		t.Errorf("%d spec citations carry a line number. Cite the section instead:\n%s",
			len(offenses), strings.Join(offenses, "\n"))
	}
}

// TestCiteOffensesCatchesEachDisguise pins the matcher itself. The scan above
// only proves the tree is clean today; if a regex breaks, the scan keeps
// passing and the drift returns unnoticed. These cases cover all three
// spellings and the near misses that must stay legal.
func TestCiteOffensesCatchesEachDisguise(t *testing.T) {
	cases := []struct {
		name string
		line string
		want int
	}{
		{"bare line", "// Spec §3.1 step 7, line 235 recovers the panic.", 1},
		{"line range", "// spec §8 lines 1624-1641 define the manifest.", 1},
		{"en dash range", "// spec lines 1624–1641 define the manifest.", 1},
		{"path colon", "// see docs/spec.md:2379 for the TTL table.", 1},
		{"section sign", "// The §1421 envelope carries a stable code.", 1},
		{"two section signs", "// Spec §2139-§2145 tune the defaults.", 2},
		{"real section", "// spec §9.4 names the meta-tool roster.", 0},
		{"deep section", "// spec §10.0.1 binds the credential subject.", 0},
		{"two digit section", "// spec §13.2 lists static resources.", 0},
		{"parser message", `// profile: line 1: unknown key`, 0},
		{"stack frame", "// runtime.go line 42 panics.", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := citeOffenses(tc.line)
			if len(got) != tc.want {
				t.Errorf("citeOffenses(%q) = %v (%d); want %d", tc.line, got, len(got), tc.want)
			}
		})
	}
}

// citeOffenses returns the citation fragments on one line that name a line
// number. It reports the fragment, not the whole line, so a line carrying two
// citations reports both.
func citeOffenses(line string) []string {
	var out []string

	out = append(out, specCiteColon.FindAllString(line, -1)...)
	out = append(out, specCiteSection.FindAllString(line, -1)...)

	for _, loc := range specCiteLine.FindAllStringIndex(line, -1) {
		left := line[max(0, loc[0]-specCiteContext):loc[0]]
		if !citesTheSpec(left) {
			continue
		}
		out = append(out, line[loc[0]:loc[1]])
	}

	return out
}

// citesTheSpec reports whether the text left of a "line NNN" fragment marks it
// as a spec reference. A section sign counts on its own: "§8 lines 1624-1641"
// never means anything else.
func citesTheSpec(left string) bool {
	return strings.Contains(strings.ToLower(left), "spec") || strings.Contains(left, "§")
}

// repoRootForCites returns the repository root, one level above the apps/
// directory that holds the Go module. The gate scans docs/ as well as the
// module, because a stale citation in a guide misleads the same way.
//
// The sanity check is on docs/ and not on docs/spec.md: the spec is listed
// under forbidden_public_paths in scripts/public-release-manifest.json, so
// stat-ing it here made both gates in this package fail in the public export,
// where docs/ is present and the spec is not. Neither gate reads the spec.
func repoRootForCites(t *testing.T) string {
	t.Helper()
	dir := moduleRoot(t) // <repo>/apps/gum
	root := filepath.Dir(filepath.Dir(dir))
	if info, err := os.Stat(filepath.Join(root, "docs")); err != nil || !info.IsDir() {
		t.Fatalf("docs/ not found above module root %s: %v", dir, err)
	}

	return root
}

// selfPath returns this file's own path so the scan skips it. The patterns
// above are spec citations by the gate's own definition.
func selfPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return file
}
