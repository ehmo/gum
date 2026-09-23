package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// agentContractDir holds the JSON transcripts the public release manifest
// publishes. Each file is one command's stdout byte for byte, so an agent
// author can diff their own run against a known-good capture. A stale capture
// is worse than none: it tells the reader a sha256 or a write plan that the
// binary they just installed does not produce.
const agentContractDir = "docs/agent-contracts"

// agentHomeRe matches the absolute home prefix in front of an install path.
// The home is environment, not contract. A capture taken under a different
// HOME still describes the same writes, and macOS resolves a temp dir through
// /private, so the comparison keeps only the path below the home.
var agentHomeRe = regexp.MustCompile(`"path": "[^"]*(/\.(?:agents|claude|codex|cursor|gemini)/)`)

// agentContractHome is the HOME the published install capture was taken
// under. It appears in the regeneration command below and nowhere else; the
// comparison normalizes it away.
const agentContractHome = "/private/tmp/gum-home"

// TestSkillsFixturesMatchLiveOutput fails when a published skills transcript
// disagrees with the binary. The skill bodies are embedded, so every edit to
// one moves its sha256 and byte count, and the capture has to move with it.
func TestSkillsFixturesMatchLiveOutput(t *testing.T) {
	cases := []struct {
		fixture string
		args    []string
	}{
		{"cli-skills-list.json", []string{"skills", "list", "--format=json"}},
		{"cli-skills-core.json", []string{"skills", "show", "core", "--format=json"}},
		{"cli-skills-hasp.json", []string{"skills", "show", "hasp", "--format=json"}},
		{"cli-skills-mcp.json", []string{"skills", "show", "mcp", "--format=json"}},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			want := readAgentContract(t, tc.fixture)
			got := runGumStdout(t, tc.args...)

			if diff := agentContractDiff(want, got); diff != "" {
				t.Errorf("%s/%s is stale: %s\n\nregenerate with:\n  gum %s > %s/%s",
					agentContractDir, tc.fixture, diff, strings.Join(tc.args, " "), agentContractDir, tc.fixture)
			}
		})
	}
}

// TestAgentsInstallFixtureMatchesLiveOutput fails when the published install
// plan no longer matches the plan the binary builds. Adding or renaming a
// shipped skill changes the plan, and this capture is what an agent author
// checks their own dry run against.
func TestAgentsInstallFixtureMatchesLiveOutput(t *testing.T) {
	const fixture = "cli-agents-install.json"
	args := []string{"agents", "install", "--dry-run", "--format=json", "--target", "all", "--features", "skills,mcp"}

	want := readAgentContract(t, fixture)
	got := runGumStdout(t, args...)

	if diff := agentContractDiff(want, got); diff != "" {
		t.Errorf("%s/%s is stale: %s\n\nregenerate with:\n  HOME=%s gum %s > %s/%s",
			agentContractDir, fixture, diff, agentContractHome, strings.Join(args, " "), agentContractDir, fixture)
	}
}

// TestAgentContractDiffCatchesEachDrift arms the two gates above. A
// comparison that passes on matching files proves nothing about what it
// rejects, so each drift the captures can suffer gets a synthetic case here,
// alongside the one difference the gate must ignore.
func TestAgentContractDiffCatchesEachDrift(t *testing.T) {
	const base = `{
  "actions": [
    {
      "kind": "skills",
      "path": "/private/tmp/gum-home/.agents/skills/gum/SKILL.md",
      "sha256": "aaaa",
      "bytes": 1250
    }
  ]
}
`

	cases := []struct {
		name  string
		got   string
		wantD bool
	}{
		{"identical", base, false},
		{"home prefix differs", strings.ReplaceAll(base, "/private/tmp/gum-home", "/private/var/folders/x/T/tmp.1"), false},
		{"sha256 moved", strings.Replace(base, `"aaaa"`, `"bbbb"`, 1), true},
		{"byte count moved", strings.Replace(base, "1250", "1264", 1), true},
		{"kind changed", strings.Replace(base, `"skills"`, `"mcp"`, 1), true},
		{"path below home changed", strings.Replace(base, "/skills/gum/SKILL.md", "/skills/gum-hasp/SKILL.md", 1), true},
		{"action dropped", "{\n  \"actions\": []\n}\n", true},
		{"trailing newline lost", strings.TrimSuffix(base, "\n"), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diff := agentContractDiff(base, tc.got)
			if (diff != "") != tc.wantD {
				t.Errorf("agentContractDiff = %q; want difference=%v", diff, tc.wantD)
			}
		})
	}
}

// agentContractDiff returns "" when got matches want once both sides have
// their home prefixes normalized, and otherwise names the first line that
// differs.
//
// The equality check comes first because the line walk cannot see every
// difference: a capture that lost its trailing newline splits into the same
// lines as one that kept it, and these files are compared byte for byte.
func agentContractDiff(want, got string) string {
	normWant, normGot := normalizeAgentHome(want), normalizeAgentHome(got)
	if normWant == normGot {
		return ""
	}

	wantLines := strings.Split(normWant, "\n")
	gotLines := strings.Split(normGot, "\n")

	for i := range max(len(wantLines), len(gotLines)) {
		w, g := "", ""
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return "line " + strconv.Itoa(i+1) + ": capture " + strconv.Quote(w) + ", live " + strconv.Quote(g)
		}
	}

	return "trailing bytes differ: capture " + strconv.Itoa(len(normWant)) + " bytes, live " + strconv.Itoa(len(normGot)) + " bytes"
}

// normalizeAgentHome replaces the absolute home prefix of every install path
// with a fixed marker.
func normalizeAgentHome(body string) string {
	return agentHomeRe.ReplaceAllString(body, `"path": "<HOME>${1}`)
}

// runGumStdout runs one gum command in process under a scratch HOME and
// returns its stdout.
func runGumStdout(t *testing.T, args ...string) string {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		t.Fatalf("gum %s: %v stderr=%q", strings.Join(args, " "), err, stderr.String())
	}

	return stdout.String()
}

// readAgentContract returns one published capture. A tree without the
// directory skips: the gate runs in the public export too, and the manifest
// decides which docs ship there. A missing file inside a present directory is
// a defect, not a trim.
func readAgentContract(t *testing.T, name string) string {
	t.Helper()

	dir := filepath.Join(repoRootForCmdCites(t), agentContractDir)
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("%s is absent from this tree", agentContractDir)
	}

	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s/%s: %v", agentContractDir, name, err)
	}

	return string(body)
}
