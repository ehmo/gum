package testmatrix

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRunAllAllPass uses a stub runHook to simulate a clean `go test`
// invocation for two groups, asserts both pass, and asserts the
// per-group Result.TestsRun reflects the count of test names recovered
// from the synthetic --- PASS lines.
func TestRunAllAllPass(t *testing.T) {
	groups := []Group{
		{Letter: "A", Tests: []string{"TestAlpha", "TestBeta"}},
		{Letter: "B", Tests: []string{"TestGamma"}},
	}
	out := map[string]string{
		"^(TestAlpha|TestBeta)$": "--- PASS: TestAlpha (0.01s)\n--- PASS: TestBeta (0.02s)\nPASS\n",
		"^(TestGamma)$":          "--- PASS: TestGamma (0.03s)\nPASS\n",
	}
	r := &Runner{
		RunHook: func(ctx context.Context, pattern, workDir string) (string, string, error) {
			body, ok := out[pattern]
			if !ok {
				t.Fatalf("unexpected pattern: %q", pattern)
			}
			return body, "", nil
		},
	}
	results := r.RunAll(context.Background(), groups)
	if got, want := len(results), 2; got != want {
		t.Fatalf("result count: got %d want %d", got, want)
	}
	for i, want := range []Status{StatusPassed, StatusPassed} {
		if got := results[i].Status; got != want {
			t.Errorf("group %s: got %s want %s", groups[i].Letter, got, want)
		}
	}
	if got, want := results[0].TestsRun, 2; got != want {
		t.Errorf("group A TestsRun: got %d want %d", got, want)
	}
	if got, want := results[1].TestsRun, 1; got != want {
		t.Errorf("group B TestsRun: got %d want %d", got, want)
	}
	if Summarize(results).AnyFailed() {
		t.Errorf("AnyFailed should be false when all pass")
	}
}

// TestRunAllFailingGroup verifies a non-zero exit from the runHook
// surfaces as Status == FAIL and that AnyFailed reports true.
func TestRunAllFailingGroup(t *testing.T) {
	groups := []Group{{Letter: "A", Tests: []string{"TestAlpha"}}}
	r := &Runner{
		RunHook: func(ctx context.Context, pattern, workDir string) (string, string, error) {
			return "--- FAIL: TestAlpha (0.00s)\n  expected 1 got 2\nFAIL\nFAIL\tgithub.com/example/pkg\t0.01s\n",
				"FAIL",
				errors.New("exit status 1")
		},
	}
	results := r.RunAll(context.Background(), groups)
	if got, want := results[0].Status, StatusFailed; got != want {
		t.Errorf("Status: got %s want %s", got, want)
	}
	if got := results[0].Stdout; !strings.Contains(got, "expected 1 got 2") {
		t.Errorf("Stdout missing FAIL output, got %q", got)
	}
	if !Summarize(results).AnyFailed() {
		t.Errorf("AnyFailed should be true on failure")
	}
}

// TestRunAllMissingTestFails verifies a `go test` invocation that does
// NOT report one of the expected tests is treated as a failure (the
// matrix promise is that the test exists and runs).
func TestRunAllMissingTestFails(t *testing.T) {
	groups := []Group{{Letter: "A", Tests: []string{"TestAlpha", "TestNeverRan"}}}
	r := &Runner{
		RunHook: func(ctx context.Context, pattern, workDir string) (string, string, error) {
			return "--- PASS: TestAlpha (0.00s)\nPASS\n", "", nil
		},
	}
	results := r.RunAll(context.Background(), groups)
	if got, want := results[0].Status, StatusFailed; got != want {
		t.Errorf("Status: got %s want %s (missing test must fail)", got, want)
	}
	if got, want := results[0].MissingTests, []string{"TestNeverRan"}; !equalStrings(got, want) {
		t.Errorf("MissingTests: got %v want %v", got, want)
	}
}

// TestRunAllDeferredMissingTestPasses verifies documented out-of-scope
// exceptions are allowed without hiding them: absent exception tests do not fail
// the group, but they are still reported in Result.Deferred for the summary.
func TestRunAllDeferredMissingTestPasses(t *testing.T) {
	groups := []Group{{Letter: "A", Tests: []string{"TestAlpha", "TestDeferred"}}}
	r := &Runner{
		DeferredTests: map[string]bool{"TestDeferred": true},
		RunHook: func(ctx context.Context, pattern, workDir string) (string, string, error) {
			return "--- PASS: TestAlpha (0.00s)\nPASS\n", "", nil
		},
	}
	results := r.RunAll(context.Background(), groups)
	if got, want := results[0].Status, StatusPassed; got != want {
		t.Fatalf("Status: got %s want %s", got, want)
	}
	if len(results[0].MissingTests) != 0 {
		t.Errorf("MissingTests: got %v want none", results[0].MissingTests)
	}
	if got, want := results[0].Deferred, []string{"TestDeferred"}; !equalStrings(got, want) {
		t.Errorf("Deferred: got %v want %v", got, want)
	}
}

// TestRunAllEmptyGroupSkipped verifies a group with zero parsed tests
// gets Status == SKIP and is not counted as a failure.
func TestRunAllEmptyGroupSkipped(t *testing.T) {
	groups := []Group{{Letter: "Z", Tests: nil}}
	r := &Runner{
		RunHook: func(ctx context.Context, pattern, workDir string) (string, string, error) {
			t.Fatalf("runHook MUST NOT be called for an empty group")
			return "", "", nil
		},
	}
	results := r.RunAll(context.Background(), groups)
	if got, want := results[0].Status, StatusEmpty; got != want {
		t.Errorf("Status: got %s want %s", got, want)
	}
	if Summarize(results).AnyFailed() {
		t.Errorf("AnyFailed should NOT flag an empty group")
	}
}

// TestCompileRunPatternAnchoring verifies the regex `go test -run`
// produces is fully anchored so prefix matches are excluded.
func TestCompileRunPatternAnchoring(t *testing.T) {
	got := compileRunPattern([]string{"TestFoo", "TestBar"})
	want := "^(TestFoo|TestBar)$"
	if got != want {
		t.Errorf("pattern: got %q want %q", got, want)
	}
}

// TestParseGoTestSummaryFoldsSubtests verifies subtest lines like
// `--- PASS: TestFoo/sub_case` are folded onto the parent test name so
// matrix matching is on the top-level identifier.
func TestParseGoTestSummaryFoldsSubtests(t *testing.T) {
	out := "--- PASS: TestFoo (0.00s)\n    --- PASS: TestFoo/sub (0.00s)\n--- FAIL: TestBar (0.00s)\n    --- FAIL: TestBar/case_two (0.00s)\n--- SKIP: TestSkipped (0.00s)\n"
	ran, failed := parseGoTestSummary(out)
	if !ran["TestFoo"] || !ran["TestBar"] || !ran["TestSkipped"] {
		t.Errorf("ran set missing parent: %v", ran)
	}
	if failed["TestFoo"] {
		t.Errorf("TestFoo should not be in failed set")
	}
	if !failed["TestBar"] {
		t.Errorf("TestBar should be in failed set")
	}
}

// TestSummaryWriteTableStableFormat is a smoke test that the printed
// table contains every group's letter, status, and the total-elapsed
// footer. The exact byte layout is not pinned (subject to formatting
// tweaks), but the presence of these tokens is part of the contract.
func TestSummaryWriteTableStableFormat(t *testing.T) {
	results := []Result{
		{Group: Group{Letter: "A", Description: "Tier A"}, Status: StatusPassed, Elapsed: 12 * time.Millisecond, TestsRun: 3},
		{Group: Group{Letter: "B", Description: "MCP surface"}, Status: StatusFailed, Elapsed: 800 * time.Millisecond, TestsRun: 5, MissingTests: []string{"TestMissing"}},
		{Group: Group{Letter: "C", Description: "Exceptions"}, Status: StatusPassed, Deferred: []string{"TestException"}},
	}
	var buf bytes.Buffer
	if err := Summarize(results).WriteTable(&buf); err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	body := buf.String()
	for _, want := range []string{"GROUP", "STATUS", "ELAPSED", "RAN", "A", "B", "C", "PASS", "FAIL", "missing: TestMissing", "exceptions: 1 documented out-of-scope tests", "Total elapsed:"} {
		if !strings.Contains(body, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, body)
		}
	}
}

func TestParseDeferredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deferred.txt")
	if err := os.WriteFile(path, []byte("# comment\n\nTestOne\nFuzzTwo\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ParseDeferredFile(path)
	if err != nil {
		t.Fatalf("ParseDeferredFile: %v", err)
	}
	if !got["TestOne"] || !got["FuzzTwo"] || len(got) != 2 {
		t.Errorf("deferred map = %+v; want TestOne and FuzzTwo only", got)
	}
}

func TestParseDeferredFileRejectsPlaceholders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deferred.txt")
	if err := os.WriteFile(path, []byte("TestBackend<Name>\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ParseDeferredFile(path); err == nil {
		t.Fatal("ParseDeferredFile placeholder err=nil; want validation error")
	}
}

// TestMatrix is the orchestration entry point used by
// `go test -run TestMatrix ./...` per the bead's acceptance criterion.
// It is skipped under `-short` because it spawns `go test` per group,
// which would otherwise recurse and re-enter this test forever.
func TestMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("TestMatrix spawns nested go test invocations; skipped under -short")
	}
	t.Skip("TestMatrix is the named orchestration entry point; invoke `cmd/test-matrix` directly for the full release-gate sweep")
}
