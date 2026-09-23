package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// cmdCiteSkipDir names directories that hold no live claim: VCS and build
// scratch, test fixtures, and the research notes, which record rounds that
// proposed commands the binary never grew.
var cmdCiteSkipDir = map[string]bool{
	".git":         true,
	"node_modules": true,
	"releases":     true,
	"research":     true,
	"testdata":     true,
	"vendor":       true,
}

// cmdCiteSkipFile names append-only history. A changelog line and a shipped
// release note record what a past version did; rewriting either to match
// today's command tree would falsify the record, so the gate reads neither.
func cmdCiteSkipFile(name string) bool {
	return name == "CHANGELOG.md" || strings.HasPrefix(name, "release-notes-")
}

// fenceRe matches a Markdown code-fence line, opening or closing.
var fenceRe = regexp.MustCompile("^\\s*```")

// cmdCiteArgStart holds the first bytes that mark a token as an argument or a
// value rather than a subcommand name. A token starting with one of these ends
// the path walk.
const cmdCiteArgStart = "-<[$\"'@|&;()*{}/."

// cmdCiteNotation holds bytes that mark a token as documentation notation
// anywhere in it, not a literal name: `install|list` is an alternation,
// `use-<strategy>` is a placeholder, and `set%s` is a format verb. None of the
// three is a line anyone runs, so each ends the walk.
const cmdCiteNotation = "|<>[]%"

// cmdCiteTrailing holds punctuation a sentence leaves on the last token, as in
// "run gum doctor:". It is stripped before the name lookup.
const cmdCiteTrailing = ":,.;)\""

// cmdCiteAbsenceMarker holds the phrases a line uses to say the command it
// names does not exist. The spec's "not built" roadmap rows and the plugin
// guide's "`gum plugin validate` does not exist" are accurate documentation of
// an absence, so a citation on such a line is not a defect. The marker must sit
// on the same line as the citation.
var cmdCiteAbsenceMarker = []string{
	"not built",
	"does not exist",
	"no such command",
	"not implemented",
}

// citesAnAbsence reports whether line declares the command it names missing.
func citesAnAbsence(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range cmdCiteAbsenceMarker {
		if strings.Contains(lower, marker) {
			return true
		}
	}

	return false
}

// TestNoDocCitesAnUnknownCommand fails when a document or a runtime message
// spells a `gum` command line the binary cannot run. The two defects this gate
// was written for were `gum config <key>=<value>` (the real syntax needs the
// `set` verb) and `gum auth use-adc` (a subcommand that never shipped). Both
// were copy-paste-ready lines that fail with "unknown command".
//
// The command tree comes from newRootCmd, so the gate tracks the binary rather
// than a checked-in list.
func TestNoDocCitesAnUnknownCommand(t *testing.T) {
	root := newRootCmd()
	// cobra attaches both at Execute time, so a tree built for inspection
	// lacks them and every `gum help` / `gum completion zsh` citation would
	// read as unknown.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	repo := repoRootForCmdCites(t)
	self := selfPathForCmdCites(t)

	var offenses []string
	for _, path := range cmdCiteScanFiles(t, repo) {
		if path == self {
			continue
		}

		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		rel, relErr := filepath.Rel(repo, path)
		if relErr != nil {
			rel = path
		}
		for _, bad := range unknownCommandCites(string(body), root, filepath.Ext(path) == ".md") {
			offenses = append(offenses, rel+":"+bad)
		}
	}

	if len(offenses) > 0 {
		sort.Strings(offenses)
		t.Errorf("%d cited command line(s) the binary cannot run:\n  %s",
			len(offenses), strings.Join(offenses, "\n  "))
	}
}

// TestCommandCitationScanCatchesEachForm arms the gate above. A scan that
// passes on a clean tree proves nothing about its detector, so every form it
// must read and every near-miss it must ignore gets a synthetic line here.
func TestCommandCitationScanCatchesEachForm(t *testing.T) {
	root := &cobra.Command{Use: "gum"}
	config := &cobra.Command{Use: "config"}
	config.AddCommand(&cobra.Command{Use: "set <key>=<value>"})
	root.AddCommand(config)
	root.AddCommand(&cobra.Command{Use: "call <op_id>"})

	cases := []struct {
		name     string
		body     string
		markdown bool
		want     []string
	}{
		{
			name: "missing verb in an inline span",
			body: "Set it with `gum config output.format=json` globally.",
			want: []string{"1: gum config output.format=json"},
		},
		{
			name: "correct line passes",
			body: "Set it with `gum config set output.format=json` globally.",
			want: nil,
		},
		{
			name: "unknown subcommand in a Go string",
			body: "const hint = \"run `gum config reset` first\"",
			want: []string{"1: gum config reset"},
		},
		{
			name:     "shell fence line",
			body:     "```bash\ngum config output.format=json\n```\n",
			markdown: true,
			want:     []string{"2: gum config output.format=json"},
		},
		{
			name:     "fence comment prose is not a citation",
			body:     "```bash\n# gum reads the catalog first\n```\n",
			markdown: true,
			want:     nil,
		},
		{
			name: "argument placeholder ends the walk",
			body: "Run `gum call <op_id> --args '{}'` directly.",
			want: nil,
		},
		{
			name: "flag ends the walk",
			body: "Run `gum config --help` for the key list.",
			want: nil,
		},
		{
			name: "prose after the binary name is not a citation",
			body: "The gum binary covers 33 services.",
			want: nil,
		},
		{
			name: "bare name alone is not a citation",
			body: "Install `gum` first.",
			want: nil,
		},
		{
			name: "unknown top-level command in an inline span",
			body: "Run `gum frobnicate` to rebuild.",
			want: []string{"1: gum frobnicate"},
		},
		{
			name: "a line declaring the command absent is not a defect",
			body: "`gum config reset` does not exist; use `gum config unset`.",
			want: nil,
		},
		{
			name:     "not-built roadmap row is not a defect",
			body:     "```text\ngum config reset    # not built\n```\n",
			markdown: true,
			want:     nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unknownCommandCites(tc.body, root, tc.markdown)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("unknownCommandCites = %v; want %v", got, tc.want)
			}
		})
	}
}

// unknownCommandCites returns one entry per cited command line in body whose
// path the tree under root cannot resolve, formatted "<line>: <cited path>".
func unknownCommandCites(body string, root *cobra.Command, markdown bool) []string {
	var out []string
	inFence := false

	for i, line := range strings.Split(body, "\n") {
		if markdown && fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}

		if citesAnAbsence(line) {
			continue
		}

		for _, chunk := range citedChunks(line, markdown && inFence) {
			bad, ok := unresolvedPath(chunk.text, root, chunk.strict)
			if !ok {
				continue
			}
			out = append(out, strconv.Itoa(i+1)+": "+bad)
		}
	}

	return out
}

// citedChunk is one candidate command line plus whether an unknown first token
// counts as a defect.
type citedChunk struct {
	text string
	// strict marks a chunk that is a command line by construction: a
	// backtick span or a fence line that opens with the binary name. Only
	// there is an unrecognised first token a defect rather than prose.
	strict bool
}

// citedChunks returns the candidate command lines on one source line: every
// backtick-delimited span, plus the whole line when it sits inside a shell
// fence. Restricting prose to backtick spans is what keeps the scan quiet;
// sentences like "gum covers 33 services" never reach the walk.
func citedChunks(line string, inFence bool) []citedChunk {
	var out []citedChunk

	if inFence {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "$ ")
		if strings.HasPrefix(trimmed, "gum ") {
			out = append(out, citedChunk{text: trimmed, strict: true})
		}
	}

	parts := strings.Split(line, "`")
	// An odd index is the inside of a backtick pair; an even index is the
	// text around it. A line with no pair yields one part and no chunk.
	for i := 1; i < len(parts); i += 2 {
		span := strings.TrimSpace(parts[i])
		span = strings.TrimPrefix(span, "$ ")
		if strings.HasPrefix(span, "gum ") {
			out = append(out, citedChunk{text: span, strict: true})
		}
	}

	return out
}

// unresolvedPath walks chunk against the command tree and returns the cited
// prefix that stops resolving, if any. The walk ends at the first flag,
// argument placeholder, or leaf command: past that point the tokens are
// arguments, not names.
func unresolvedPath(chunk string, root *cobra.Command, strict bool) (string, bool) {
	fields := strings.Fields(chunk)
	if len(fields) < 2 {
		return "", false
	}

	cur := root
	cited := []string{fields[0]}
	for _, raw := range fields[1:] {
		tok := strings.TrimRight(raw, cmdCiteTrailing)
		if tok == "" || strings.ContainsAny(tok[:1], cmdCiteArgStart) {
			return "", false
		}
		if strings.ContainsAny(tok, cmdCiteNotation) {
			return "", false
		}
		if len(childNames(cur)) == 0 {
			return "", false
		}

		child := childByName(cur, tok)
		if child == nil {
			if cur == root && !strict {
				return "", false
			}
			return strings.Join(append(cited, tok), " "), true
		}

		cur = child
		cited = append(cited, tok)
	}

	return "", false
}

// childByName returns the subcommand of cur named or aliased tok.
func childByName(cur *cobra.Command, tok string) *cobra.Command {
	for _, child := range cur.Commands() {
		if child.Name() == tok {
			return child
		}
		for _, alias := range child.Aliases {
			if alias == tok {
				return child
			}
		}
	}

	return nil
}

// childNames returns the names of cur's subcommands.
func childNames(cur *cobra.Command) []string {
	out := make([]string, 0, len(cur.Commands()))
	for _, child := range cur.Commands() {
		out = append(out, child.Name())
	}

	return out
}

// cmdCiteScanFiles returns the Go sources under the module and the Markdown
// pages outside docs/research, plus the top-level pages a reader reaches
// first. Missing paths are skipped rather than fatal: the public export ships
// a subset of docs/, and this gate runs there too.
func cmdCiteScanFiles(t *testing.T, repo string) []string {
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
				if cmdCiteSkipDir[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) == ext && !cmdCiteSkipFile(d.Name()) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}

	walk(filepath.Join(repo, "apps", "gum"), ".go")
	walk(filepath.Join(repo, "docs"), ".md")
	walk(filepath.Join(repo, "skills"), ".md")
	for _, page := range []string{"README.md", "CONTRIBUTING.md"} {
		path := filepath.Join(repo, page)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
	}

	if len(files) == 0 {
		t.Fatalf("no files to scan under %s", repo)
	}

	return files
}

// repoRootForCmdCites returns the repository root, one level above the apps/
// directory that holds the Go module.
func repoRootForCmdCites(t *testing.T) string {
	t.Helper()

	dir := filepath.Dir(selfPathForCmdCites(t))
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(filepath.Dir(dir))
		}
		dir = filepath.Dir(dir)
	}

	t.Fatal("could not locate go.mod ancestor")
	return ""
}

// selfPathForCmdCites returns this file's own path so the scan skips it. The
// synthetic lines in the detector table are citations by the gate's own
// definition.
func selfPathForCmdCites(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	return file
}
