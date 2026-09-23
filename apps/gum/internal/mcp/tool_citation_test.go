package mcp

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// toolCitationRe matches prose that tells a reader to call an MCP tool: the
// tool name, optionally backticked, followed by the word "tool". It catches
// both "the MCP gum.call tool" in a Go message constant and "the `gum.call`
// tool" in a Markdown page.
//
// The trailing word is what keeps the pattern narrow. Bare `gum.` strings in
// this tree are also keyring service names (`gum.gum_oauth`), MCP resource
// names (`gum.status`), and InputRequests keys (`gum.roots`), none of which
// name a tool and none of which this gate should read.
var toolCitationRe = regexp.MustCompile("`?(gum\\.[a-z_][a-z0-9_]*)`? tool")

// citeScanSkipDir names directories that hold no live claim: build and VCS
// scratch, test fixtures, and the research notes, which are point-in-time
// records of rounds that proposed tools the tree never grew.
var citeScanSkipDir = map[string]bool{
	".git":         true,
	"node_modules": true,
	"research":     true,
	"testdata":     true,
	"vendor":       true,
}

// citeScanSkipFile names append-only history. A changelog line and a shipped
// release note record what a past version said, including the dead tool name
// a fix removed; rewriting either to match today's roster would falsify the
// record, so the walk reads neither.
func citeScanSkipFile(name string) bool {
	return name == "CHANGELOG.md" || strings.HasPrefix(name, "release-notes-")
}

// TestNoProseCitesAnUnregisteredTool fails when a runtime message or a live
// document sends the reader to an MCP tool the server does not register. A
// refusal that names a tool nobody can call leaves the caller with no next
// step, and a served roster is the only authority on which names exist.
//
// The scan covers Go sources and Markdown together because the same stale
// name misleads from either place, and the Go half is the half a user hits at
// runtime.
func TestNoProseCitesAnUnregisteredTool(t *testing.T) {
	registered := registeredToolNames(t)
	root := repoRootForToolCites(t)
	self := selfPathForToolCites(t)

	var offenses []string
	for _, path := range toolCiteScanFiles(t, root) {
		if path == self {
			continue
		}

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		for _, cite := range unregisteredToolCites(string(body), registered) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			offenses = append(offenses, rel+":"+cite.where+" cites "+cite.name)
		}
	}

	if len(offenses) > 0 {
		sort.Strings(offenses)
		t.Errorf("prose cites %d MCP tool name(s) the server does not register:\n  %s\n\nregistered: %s",
			len(offenses), strings.Join(offenses, "\n  "), strings.Join(sortedNames(registered), ", "))
	}
}

// TestToolCitationScanCatchesEachForm arms the gate above. A scan that passes
// on a clean tree proves nothing about its detector, so each recognised form
// gets a synthetic line here, alongside the near-misses the pattern must not
// read as a tool citation.
func TestToolCitationScanCatchesEachForm(t *testing.T) {
	registered := map[string]bool{"gum.read": true, "gum.write": true}

	cases := []struct {
		name string
		body string
		want []string
	}{
		{"go message constant", `const m = "use the MCP gum.call tool or CLI directly"`, []string{"gum.call"}},
		{"backticked markdown", "The CLI and the MCP `gum.call` tool are unaffected.", []string{"gum.call"}},
		{"registered name passes", "Call the `gum.read` tool for a read-class op.", nil},
		{"two offenders", "the gum.call tool, then the gum.invoke tool", []string{"gum.call", "gum.invoke"}},
		{"resource name is not a tool citation", `const r = "gum.status resource"`, nil},
		{"keyring service is not a tool citation", `const svc = "gum.gum_oauth"`, nil},
		{"inputrequests key is not a tool citation", `const k = "gum.roots"`, nil},
		{"prefix without the trailing word", "See gum.call for details.", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, cite := range unregisteredToolCites(tc.body, registered) {
				got = append(got, cite.name)
			}

			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("unregisteredToolCites = %v; want %v", got, tc.want)
			}
		})
	}
}

// toolCite is one offending citation: the name and the line it sits on.
type toolCite struct {
	name  string
	where string
}

// unregisteredToolCites returns every tool citation in body whose name is not
// in registered, in file order.
func unregisteredToolCites(body string, registered map[string]bool) []toolCite {
	var out []toolCite

	for i, line := range strings.Split(body, "\n") {
		for _, m := range toolCitationRe.FindAllStringSubmatch(line, -1) {
			if registered[m[1]] {
				continue
			}
			out = append(out, toolCite{name: m[1], where: strconv.Itoa(i + 1)})
		}
	}

	return out
}

// registeredToolNames returns the names on the live tools/list roster.
func registeredToolNames(t *testing.T) map[string]bool {
	t.Helper()

	names := map[string]bool{}
	for _, tool := range rawToolsList(t) {
		var name string
		if err := json.Unmarshal(tool["name"], &name); err != nil {
			t.Fatalf("decode tool name: %v", err)
		}
		names[name] = true
	}

	return names
}

// toolCiteScanFiles returns the Go sources under the module and the Markdown
// pages outside docs/research, plus the two top-level pages a reader reaches
// first. Missing paths are skipped rather than fatal: the public export ships
// a subset of docs/, and this gate runs there too.
func toolCiteScanFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string
	walk := func(base string, ext string) {
		if _, err := os.Stat(base); err != nil {
			return
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if citeScanSkipDir[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) == ext && !citeScanSkipFile(d.Name()) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}

	walk(filepath.Join(root, "apps", "gum"), ".go")
	walk(filepath.Join(root, "docs"), ".md")
	for _, page := range []string{"README.md", "CONTRIBUTING.md"} {
		path := filepath.Join(root, page)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
	}

	if len(files) == 0 {
		t.Fatalf("no files to scan under %s", root)
	}

	return files
}

// repoRootForToolCites returns the repository root, one level above the apps/
// directory that holds the Go module.
func repoRootForToolCites(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	dir := filepath.Dir(file)
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(filepath.Dir(dir))
		}
		dir = filepath.Dir(dir)
	}

	t.Fatal("could not locate go.mod ancestor")
	return ""
}

// selfPathForToolCites returns this file's own path so the scan skips it. The
// synthetic lines in the detector table are citations by the gate's own
// definition.
func selfPathForToolCites(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return file
}

// sortedNames returns the keys of set in sorted order.
func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)

	return out
}
