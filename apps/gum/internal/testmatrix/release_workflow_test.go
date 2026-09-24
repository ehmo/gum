package testmatrix

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// minAllowlistReason is the shortest justification a govulncheck allowlist
// entry may carry. A one-word reason is not one a reviewer can check.
const minAllowlistReason = 40

// docs/test-matrix.md, release-pipeline row. The release gates live in
// GitHub Actions manifests, so the only thing that can hold them is a test
// that reads those manifests. These
// two tests assert the exact command lines and the job dependency graph: a
// deleted step, a weakened flag, or a broken `needs:` edge fails here rather
// than silently shipping an unraced or unscanned tag.
//
// The module has no YAML parser dependency, so the assertions are textual and
// deliberately exact.

// workflowFile returns the contents of .github/workflows/<name>.
func workflowFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(repoRootForWorkflows(t), ".github", "workflows", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// repoRootForWorkflows walks up from the module root to the directory holding
// .github, which is the repo root rather than the Go module root.
func repoRootForWorkflows(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, ".github", "workflows")); statErr == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal(".github/workflows not found above the test's working directory")
		}
		dir = next
	}
}

// jobBlock returns the YAML text of one top-level job in a workflow: every
// line from `  <name>:` up to the next line at the same indent.
func jobBlock(t *testing.T, workflow, job string) string {
	t.Helper()
	lines := strings.Split(workflow, "\n")
	start := -1
	for i, line := range lines {
		if line == "  "+job+":" {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("job %q not found; the release gate cannot be asserted", job)
	}
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.TrimSpace(line) != "" {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

// TestRaceModeReleaseGate pins that a tag cannot publish without a race build
// on the pinned toolchain, and that the publishing job transitively depends on
// it.
func TestRaceModeReleaseGate(t *testing.T) {
	wf := workflowFile(t, "release.yml")
	pre := jobBlock(t, wf, "pre-release-tests")

	for _, want := range []string{
		"go test -race -timeout 600s ./...",
		"go-version: '1.26.x'",
		"run: make fmt-check",
		"run: go vet ./...",
	} {
		if !strings.Contains(pre, want) {
			t.Errorf("pre-release-tests does not run %q", want)
		}
	}

	// The gate is worthless if nothing downstream waits for it.
	if !strings.Contains(jobBlock(t, wf, "govulncheck"), "needs: pre-release-tests") {
		t.Error("govulncheck does not need pre-release-tests; a red race build would not block the scan")
	}
	if !strings.Contains(jobBlock(t, wf, "goreleaser"), "needs: [govulncheck, docs-live]") {
		t.Error("goreleaser does not need govulncheck; a tag could publish past a failed gate")
	}

	// The same race command guards every push on the test workflow, so main
	// is never red in a way the release job would be the first to notice.
	if !strings.Contains(workflowFile(t, "test.yml"), "go test -race -timeout 300s ./...") {
		t.Error("test.yml no longer runs the race suite on push")
	}
}

// gateScript returns the contents of scripts/check-govulncheck.py.
func gateScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRootForWorkflows(t), "scripts", "check-govulncheck.py")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// TestGovulncheckPipeline pins the vulnerability scan: a pinned scanner
// version, the whole module in scope, and no `|| true` softening. The scan runs
// through scripts/check-govulncheck.py, which fails on any finding gum's build
// can reach and on any module-only finding the allowlist does not record.
func TestGovulncheckPipeline(t *testing.T) {
	block := jobBlock(t, workflowFile(t, "release.yml"), "govulncheck")

	if !strings.Contains(block, "scripts/check-govulncheck.py") {
		t.Error("the scan does not run the govulncheck gate")
	}
	if !regexp.MustCompile(`go install golang\.org/x/vuln/cmd/govulncheck@v\d+\.\d+\.\d+`).MatchString(block) {
		t.Error("govulncheck is not installed at a pinned version; a floating @latest makes the gate unreproducible")
	}
	for _, soften := range []string{"|| true", "continue-on-error"} {
		if strings.Contains(block, soften) {
			t.Errorf("the govulncheck job contains %q; the gate does not block", soften)
		}
	}

	gate := gateScript(t)
	if !strings.Contains(gate, `"./..."`) {
		t.Error("the gate does not cover ./...")
	}
	if !strings.Contains(gate, `"-scan", "symbol"`) {
		t.Error("the gate does not scan at symbol level; a coarser scan cannot separate reachable from module-only")
	}
}

// TestGovulncheckAllowlistShape pins the one file that can silence a finding.
// Every entry names the advisory, the module it sits in, a reason a reviewer
// can check, and the date it was recorded. The push workflow runs the same
// gate as the tag, so a new finding fails a PR rather than a release.
func TestGovulncheckAllowlistShape(t *testing.T) {
	path := filepath.Join(repoRootForWorkflows(t), "scripts", "govulncheck-allowlist.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var allowlist struct {
		ModuleOnly []struct {
			ID       string `json:"id"`
			Module   string `json:"module"`
			Reason   string `json:"reason"`
			Recorded string `json:"recorded"`
		} `json:"module_only"`
	}
	if err := json.Unmarshal(raw, &allowlist); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	advisory := regexp.MustCompile(`^GO-\d{4}-\d+$`)
	day := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	seen := make(map[string]bool, len(allowlist.ModuleOnly))
	for i, entry := range allowlist.ModuleOnly {
		if !advisory.MatchString(entry.ID) {
			t.Errorf("entry %d has id %q, which is not a Go advisory id", i, entry.ID)
		}
		if entry.Module == "" {
			t.Errorf("entry %s names no module", entry.ID)
		}
		if len(entry.Reason) < minAllowlistReason {
			t.Errorf("entry %s carries no reason a reviewer can check", entry.ID)
		}
		if !day.MatchString(entry.Recorded) {
			t.Errorf("entry %s records %q, not a YYYY-MM-DD date", entry.ID, entry.Recorded)
		}
		if seen[entry.ID] {
			t.Errorf("entry %s appears twice", entry.ID)
		}
		seen[entry.ID] = true
	}

	push := jobBlock(t, workflowFile(t, "govulncheck.yml"), "govulncheck")
	if !strings.Contains(push, "scripts/check-govulncheck.py") {
		t.Error("the push workflow does not run the same gate as the tag")
	}
}

// TestFuzzWorkflowTargetsExist pins the six scheduled fuzz targets from row
// 209: each matrix entry names a package that really holds that Fuzz function,
// and each runs for the documented 60s.
func TestFuzzWorkflowTargetsExist(t *testing.T) {
	wf := workflowFile(t, "fuzz.yml")

	if !strings.Contains(wf, "-fuzztime=60s") {
		t.Error("the fuzz workflow no longer runs 60s per target")
	}

	entry := regexp.MustCompile(`package: (\S+)\n\s+target: (\w+)`)
	found := entry.FindAllStringSubmatch(wf, -1)
	if len(found) != 6 {
		t.Fatalf("fuzz matrix has %d entries; the test matrix pins six targets", len(found))
	}

	want := map[string]string{
		"FuzzToonDecode":     "./internal/output/toon/...",
		"FuzzJCSCanonical":   "./internal/output/jcs/...",
		"FuzzJCSIdempotent":  "./internal/output/jcs/...",
		"FuzzPluginManifest": "./internal/plugins",
		"FuzzParseArgs":      "./internal/cli/callargs/...",
		"FuzzParse":          "./internal/output/profile/...",
	}
	root := moduleRootForWorkflows(t)
	for _, m := range found {
		pkg, target := m[1], m[2]
		if wantPkg, known := want[target]; !known {
			t.Errorf("fuzz target %q is not one of the six test-matrix pins", target)
			continue
		} else if pkg != wantPkg {
			t.Errorf("target %s runs against %s; the test matrix pins %s", target, pkg, wantPkg)
		}
		dir := filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(strings.TrimPrefix(pkg, "./"), "/...")))
		if !dirDeclaresFunc(t, dir, "func "+target+"(") {
			t.Errorf("no `func %s(` under %s; the scheduled job would fail on a missing target", target, dir)
		}
	}
}

// moduleRootForWorkflows returns the Go module root (apps/gum).
func moduleRootForWorkflows(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found")
		}
		dir = next
	}
}

// dirDeclaresFunc reports whether any *_test.go directly in dir, or in a
// subdirectory, declares the given function signature prefix.
func dirDeclaresFunc(t *testing.T, dir, sig string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || found {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr == nil && strings.Contains(string(raw), sig) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return found
}
