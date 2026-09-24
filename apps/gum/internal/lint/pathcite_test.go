// pathcite_test.go — a backticked repo path in prose must resolve to a file.
//
// A citation that names a path is the reader's next step: it tells them where
// the behaviour lives. When the path is wrong, the step is dead, and nothing
// in the build notices. A 2026-09 sweep found three such citations, one of
// them in the spec's normative §14 package-boundary table, naming
// `internal/adapters/plugin`, a package this tree has never had.
//
// Scope: a backticked token that starts with one of the tree's top-level
// directories and carries no glob, placeholder or symbol suffix. The gate
// resolves it against the repository root and against the Go module root, so
// both `docs/spec.md` and the module-relative `internal/dispatch` work. A
// line that says the path is absent is exempt, because a citation of
// something that does not exist is the claim, not a mistake.
//
// The public export ships a subset of docs/, so this gate is stricter there:
// a Go comment that backticks a page the manifest withholds is a dead pointer
// for a reader of the public repository, and the export's own contract check
// covers only Markdown. Name such a page without backticks, or without the
// docs/ prefix, the way docs/test-matrix.md writes `spec.md`.
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

// backtickedToken matches one inline-code span. Go comments and Markdown
// prose both wrap a path this way, and both are in scope.
var backtickedToken = regexp.MustCompile("`([^`\n]+)`")

// pathCiteCandidate matches a token shaped like a path into this tree. The
// leading alternation is what keeps the gate off package paths that belong to
// other modules and off prose that happens to contain a slash.
var pathCiteCandidate = regexp.MustCompile(`^(?:apps|docs|scripts|infra|data|\.github|internal|cmd|gen)/[A-Za-z0-9_./+-]+$`)

// pathCitePlaceholder marks a token that names a shape rather than a file: a
// glob, an elision, an angle-bracket slot, or a version placeholder.
var pathCitePlaceholder = []string{"*", "...", "<", ">", "X.Y.Z"}

// pathCiteVersionSegment matches a final segment that is an API version, as in
// the Google Docs API's `docs/v1`. Those are URL paths, not files.
var pathCiteVersionSegment = regexp.MustCompile(`^v\d`)

// pathCiteFileExts are the suffixes this tree actually stores. A final
// segment whose dot suffix is absent from this set is a symbol citation, as in
// `internal/embed.Save` or `internal/dispatch.Kernel`, and names no file.
var pathCiteFileExts = map[string]bool{
	".bend": true, ".css": true, ".csv": true, ".go": true, ".html": true,
	".js": true, ".json": true, ".jsonl": true, ".md": true, ".mjs": true,
	".mod": true, ".plist": true, ".proto": true, ".py": true, ".rb": true,
	".sh": true, ".sql": true, ".sum": true, ".tmpl": true, ".toml": true,
	".ts": true, ".txt": true, ".yaml": true, ".yml": true,
}

// pathCiteAbsenceMarkers are the phrasings that make a dead path the point of
// the sentence. The spec and the audit notes both cite packages this tree
// deliberately does not have, to say so.
var pathCiteAbsenceMarkers = []string{
	"does not exist", "doesn't exist", "never existed", "nonexistent",
	"non-existent", "no longer exists", "not built", "unimplemented",
	"no such", "would live", "would be",
}

// pathCiteAbsenceWindow is how far before the token a bare "no" still reads as
// a denial of the path, as in "no `internal/retry` package".
const pathCiteAbsenceWindow = 45

// TestNoCitationNamesAMissingPath fails when a backticked repo path in a live
// document or Go source resolves to nothing. The fix is to name the real path,
// or to say in the same line that the path is absent.
func TestNoCitationNamesAMissingPath(t *testing.T) {
	root := repoRootForCites(t)
	module := moduleRoot(t)
	self := pathCiteSelfPath(t)

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

		for _, cite := range deadPathCitesIn(string(data), root, module) {
			offenses = append(offenses, fmt.Sprintf("%s:%d: %s", relPath(root, path), cite.line, cite.token))
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(offenses) > 0 {
		t.Errorf("%d citation(s) name a path that does not exist:\n  %s\n\nname the real path, or say in the same line that the path is absent",
			len(offenses), strings.Join(offenses, "\n  "))
	}
}

// TestPathCitesCatchesEachShape arms the gate above. A clean tree proves
// nothing about the detector, so every shape it must read and every shape it
// must ignore gets a synthetic line here. The roots are a fixture rather than
// this repository, so the table reads the same in the source tree and in the
// public export, which ships a subset of docs/.
func TestPathCitesCatchesEachShape(t *testing.T) {
	root, module := pathCiteFixtureRoots(t)

	cases := []struct {
		name string
		body string
		want []string
	}{
		{"dead package in a table cell", "| `internal/adapters/plugin` | spawns plugins |", []string{"internal/adapters/plugin"}},
		{"dead doc page", "See `docs/nope.md` for the rollout.", []string{"docs/nope.md"}},
		{"dead script", "Run `scripts/export-nothing.py` first.", []string{"scripts/export-nothing.py"}},
		{"two on one line", "`docs/nope.md` and `docs/alsonope.md`", []string{"docs/nope.md", "docs/alsonope.md"}},
		{"live doc passes", "See `docs/spec.md` §14.", nil},
		{"live module-relative package passes", "`internal/dispatch` owns the lifecycle.", nil},
		{"live file under the module passes", "`apps/gum/go.mod` pins the toolchain.", nil},
		{"glob is not a path", "The generator writes `gen/dispatch/stub_*.go`.", nil},
		{"elision is not a path", "Executors live in `internal/adapters/...`.", nil},
		{"angle slot is not a path", "Write `docs/release-notes-<version>.md`.", nil},
		{"version placeholder is not a path", "Tag `docs/release-notes-vX.Y.Z.md`.", nil},
		{"symbol citation is not a path", "`internal/embed.Save` writes the index.", nil},
		{"api version segment is not a path", "The Docs API is `docs/v1`.", nil},
		{"absence phrasing is exempt", "There is no `internal/retry` package; callers retry inline.", nil},
		{"explicit denial is exempt", "`internal/adapters/plugin` does not exist.", nil},
		{"other module path is out of scope", "`github.com/ehmo/gum/internal/nope` is upstream.", nil},
		{"prose with a slash is not a path", "Use `json|text` for the format.", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, cite := range deadPathCitesIn(tc.body, root, module) {
				got = append(got, cite.token)
			}

			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("deadPathCitesIn(%q) = %v; want %v", tc.body, got, tc.want)
			}
		})
	}
}

// pathCiteFixtureRoots builds the two roots the detector resolves against: a
// repository root holding docs/spec.md, and a module root holding
// internal/dispatch and go.mod. Every live path in the table above is one of
// these, so the table does not depend on which pages a tree ships.
func pathCiteFixtureRoots(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	module := filepath.Join(root, "apps", "gum")

	for _, dir := range []string{
		filepath.Join(root, "docs"),
		filepath.Join(module, "internal", "dispatch"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	for _, file := range []string{
		filepath.Join(root, "docs", "spec.md"),
		filepath.Join(module, "go.mod"),
	} {
		if err := os.WriteFile(file, []byte("fixture\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}

	return root, module
}

// pathCitation is one offending citation: the line it sits on and the token.
type pathCitation struct {
	line  int
	token string
}

// deadPathCitesIn returns every backticked path citation in body that resolves
// under neither root, in file order.
func deadPathCitesIn(body, root, module string) []pathCitation {
	var out []pathCitation

	for i, line := range strings.Split(body, "\n") {
		for _, m := range backtickedToken.FindAllStringSubmatchIndex(line, -1) {
			token := line[m[2]:m[3]]
			if !pathCiteResolvable(token) {
				continue
			}
			if pathCiteExists(token, root, module) {
				continue
			}
			if pathCiteDeniesToken(line, m[0]) {
				continue
			}

			out = append(out, pathCitation{line: i + 1, token: token})
		}
	}

	return out
}

// pathCiteResolvable reports whether a token is a citation the gate can check:
// shaped like a path into this tree, with no placeholder and no symbol suffix.
func pathCiteResolvable(token string) bool {
	if !pathCiteCandidate.MatchString(token) {
		return false
	}
	for _, marker := range pathCitePlaceholder {
		if strings.Contains(token, marker) {
			return false
		}
	}

	last := token[strings.LastIndex(token, "/")+1:]
	if pathCiteVersionSegment.MatchString(last) {
		return false
	}
	if dot := strings.LastIndex(last, "."); dot > 0 {
		return pathCiteFileExts[last[dot:]]
	}

	return true
}

// pathCiteExists reports whether the token resolves against the repository
// root or the Go module root. Module-relative citations such as
// `internal/dispatch` need the second root.
func pathCiteExists(token, root, module string) bool {
	for _, base := range []string{root, module} {
		if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(token))); err == nil {
			return true
		}
	}

	return false
}

// pathCiteDeniesToken reports whether the line says the path is absent. at is
// the token's offset, so a bare "no" only counts when it sits just before it.
func pathCiteDeniesToken(line string, at int) bool {
	lower := strings.ToLower(line)
	for _, marker := range pathCiteAbsenceMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	start := at - pathCiteAbsenceWindow
	if start < 0 {
		start = 0
	}

	return strings.Contains(lower[start:at], "no ")
}

// pathCiteSelfPath returns this file's own path so the walk skips it. The
// detector table above is a page of dead citations by the gate's definition.
func pathCiteSelfPath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return file
}
