package testmatrix

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Matrix row 209. The release gates live in GitHub Actions manifests, so the
// only thing that can hold them is a test that reads those manifests. These
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

// TestGovulncheckPipeline pins the vulnerability scan: a pinned scanner
// version, the whole module in scope, and no `|| true` softening.
func TestGovulncheckPipeline(t *testing.T) {
	block := jobBlock(t, workflowFile(t, "release.yml"), "govulncheck")

	if !strings.Contains(block, "run: govulncheck ./...") {
		t.Error("the scan does not cover ./...")
	}
	if !regexp.MustCompile(`go install golang\.org/x/vuln/cmd/govulncheck@v\d+\.\d+\.\d+`).MatchString(block) {
		t.Error("govulncheck is not installed at a pinned version; a floating @latest makes the gate unreproducible")
	}
	for _, soften := range []string{"|| true", "continue-on-error"} {
		if strings.Contains(block, soften) {
			t.Errorf("the govulncheck job contains %q; the gate does not block", soften)
		}
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
		t.Fatalf("fuzz matrix has %d entries; row 209 pins six targets", len(found))
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
			t.Errorf("fuzz target %q is not one of the six row 209 pins", target)
			continue
		} else if pkg != wantPkg {
			t.Errorf("target %s runs against %s; row 209 pins %s", target, pkg, wantPkg)
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
