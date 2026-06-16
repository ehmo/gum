package testmatrix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Result is the per-group outcome from running one group's tests against
// the module under test.
type Result struct {
	Group        Group
	Status       Status
	Elapsed      time.Duration
	TestsRun     int      // count reported by `go test` ("--- PASS/--- FAIL" lines)
	MissingTests []string // expected tests that did not appear in any reported run
	Deferred     []string // missing expected tests explicitly deferred for this release scope
	Stdout       string   // raw stdout when Status == StatusFailed (truncated)
	Stderr       string   // raw stderr when Status == StatusFailed (truncated)
}

// Status is the per-group pass/fail summary.
type Status string

const (
	StatusPassed Status = "PASS"
	StatusFailed Status = "FAIL"
	StatusEmpty  Status = "SKIP" // group has no runnable tests in the matrix
)

// runHookFn is the indirection seam for tests: it executes the configured
// `go test` invocation and returns (combined stdout, combined stderr,
// exit-error). The default implementation is realGoTest.
type runHookFn func(ctx context.Context, runPattern string, workDir string) (stdout, stderr string, err error)

// Runner orchestrates per-group `go test` invocations.
//
// Zero value works: it runs `go test -run <pattern> -count=1 ./...` from
// the current working directory. Callers MAY override fields for testing
// (RunHook) or to redirect output (WorkDir).
type Runner struct {
	WorkDir       string          // module root containing go.mod; defaults to "."
	RunHook       runHookFn       // override for tests; nil = realGoTest
	DeferredTests map[string]bool // expected tests missing by documented release-scope deferral
}

// RunAll executes every group in g in source order and returns the
// per-group results. Cancelling ctx aborts the in-flight `go test`
// invocation.
func (r *Runner) RunAll(ctx context.Context, groups []Group) []Result {
	results := make([]Result, len(groups))
	for i, group := range groups {
		results[i] = r.runGroup(ctx, group)
	}
	return results
}

func (r *Runner) runGroup(ctx context.Context, g Group) Result {
	start := time.Now()
	res := Result{Group: g}
	if len(g.Tests) == 0 {
		res.Status = StatusEmpty
		res.Elapsed = time.Since(start)
		return res
	}
	pattern := compileRunPattern(g.Tests)
	hook := r.RunHook
	if hook == nil {
		hook = realGoTest
	}
	stdout, stderr, err := hook(ctx, pattern, r.workDir())
	res.Elapsed = time.Since(start)
	ran, _ := parseGoTestSummary(stdout)
	res.TestsRun = len(ran)
	res.MissingTests, res.Deferred = diffMissing(g.Tests, ran, r.DeferredTests)
	if err != nil {
		res.Status = StatusFailed
		res.Stdout = truncate(stdout, 8*1024)
		res.Stderr = truncate(stderr, 4*1024)
		return res
	}
	if len(res.MissingTests) > 0 {
		res.Status = StatusFailed
		res.Stdout = truncate(stdout, 8*1024)
		res.Stderr = truncate(stderr, 4*1024)
		return res
	}
	res.Status = StatusPassed
	return res
}

func (r *Runner) workDir() string {
	if r.WorkDir == "" {
		return "."
	}
	return r.WorkDir
}

// compileRunPattern builds an anchored regex covering every test in names.
// `go test -run` accepts a regex matched against the unqualified test name,
// so anchoring with ^...$ prevents prefix matches (e.g. `TestFoo` matching
// `TestFooBar`).
func compileRunPattern(names []string) string {
	if len(names) == 0 {
		return "^$"
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, regexp.QuoteMeta(n))
	}
	return "^(" + strings.Join(parts, "|") + ")$"
}

// realGoTest runs `go test -run <pattern> -count=1 -v ./...` from workDir.
// `-v` is required: without it go test omits the `--- PASS: TestName` lines
// that parseGoTestSummary regexes against, so every group would falsely
// report RAN=0 and look like a missed-coverage failure. The exit code
// carries pass/fail; we surface a non-nil error only when go test itself
// reports a failure (so absent tests are flagged separately).
func realGoTest(ctx context.Context, runPattern string, workDir string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "go", "test", "-run", runPattern, "-count=1", "-v", "./...")
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// goTestPassRe captures the test name from a `--- PASS: TestFoo (0.00s)` line.
// goTestFailRe captures the same for FAIL lines. The leading `---` is the
// canonical end-of-test marker emitted by `go test`.
var (
	goTestPassRe = regexp.MustCompile(`^\s*--- PASS:\s+([A-Za-z0-9_]+)`)
	goTestFailRe = regexp.MustCompile(`^\s*--- FAIL:\s+([A-Za-z0-9_]+)`)
	goTestSkipRe = regexp.MustCompile(`^\s*--- SKIP:\s+([A-Za-z0-9_]+)`)
)

// parseGoTestSummary returns the set of unqualified test names that
// reported PASS, FAIL, or SKIP plus the set that reported FAIL. Skipped tests
// count as present for matrix-inventory purposes: a deliberate t.Skip with a
// diagnostic is a tracked scope decision, not a missing proof artifact. Subtests
// are folded onto their parent's first dotted component so callers can match
// against the matrix test name.
func parseGoTestSummary(out string) (ran, failed map[string]bool) {
	ran = map[string]bool{}
	failed = map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if m := goTestPassRe.FindStringSubmatch(line); m != nil {
			ran[topLevel(m[1])] = true
		} else if m := goTestFailRe.FindStringSubmatch(line); m != nil {
			name := topLevel(m[1])
			ran[name] = true
			failed[name] = true
		} else if m := goTestSkipRe.FindStringSubmatch(line); m != nil {
			ran[topLevel(m[1])] = true
		}
	}
	return ran, failed
}

func topLevel(name string) string {
	if i := strings.Index(name, "/"); i > 0 {
		return name[:i]
	}
	return name
}

func diffMissing(expected []string, ran map[string]bool, deferred map[string]bool) (missing, deferredMissing []string) {
	for _, name := range expected {
		if ran[name] {
			continue
		}
		if deferred[name] {
			deferredMissing = append(deferredMissing, name)
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	sort.Strings(deferredMissing)
	return missing, deferredMissing
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("\n... [truncated %d bytes]", len(s)-n)
}

// Summary aggregates per-group results into the table-shape printed by
// cmd/test-matrix.
type Summary struct {
	Results []Result
	Total   time.Duration
}

// Summarize aggregates per-group results into a Summary view.
func Summarize(results []Result) Summary {
	var total time.Duration
	for _, r := range results {
		total += r.Elapsed
	}
	return Summary{Results: results, Total: total}
}

// AnyFailed reports whether any group has Status == StatusFailed. Used by
// cmd/test-matrix for the process exit code.
func (s Summary) AnyFailed() bool {
	for _, r := range s.Results {
		if r.Status == StatusFailed {
			return true
		}
	}
	return false
}

// WriteTable prints a fixed-width per-group summary to w. The format is
// stable enough for a golden test or a CI log to grep for.
func (s Summary) WriteTable(w io.Writer) error {
	const groupCol = 6
	const statusCol = 6
	const elapsedCol = 12
	const ranCol = 8
	const descCol = 60

	header := fmt.Sprintf("%-*s %-*s %-*s %-*s %-*s\n",
		groupCol, "GROUP",
		statusCol, "STATUS",
		elapsedCol, "ELAPSED",
		ranCol, "RAN",
		descCol, "DESCRIPTION",
	)
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	if _, err := io.WriteString(w, strings.Repeat("-", len(header)-1)+"\n"); err != nil {
		return err
	}
	for _, r := range s.Results {
		line := fmt.Sprintf("%-*s %-*s %-*s %-*d %-*s\n",
			groupCol, r.Group.Letter,
			statusCol, string(r.Status),
			elapsedCol, fmtDuration(r.Elapsed),
			ranCol, r.TestsRun,
			descCol, truncate(r.Group.Description, descCol),
		)
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
		if len(r.MissingTests) > 0 {
			if _, err := fmt.Fprintf(w, "       missing: %s\n", strings.Join(r.MissingTests, ", ")); err != nil {
				return err
			}
		}
		if len(r.Deferred) > 0 {
			if _, err := fmt.Fprintf(w, "       exceptions: %d documented out-of-scope tests\n", len(r.Deferred)); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(w, "\nTotal elapsed: %s\n", fmtDuration(s.Total)); err != nil {
		return err
	}
	return nil
}

// ParseDeferredFile reads a newline-delimited allowlist of matrix test names
// that are explicitly outside the current release scope. Blank lines and #
// comments are ignored. Every non-comment line must be a concrete Test*/Fuzz*
// symbol; placeholders such as TestBackend<Name> are rejected.
func ParseDeferredFile(path string) (map[string]bool, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("testmatrix: open deferred manifest %s: %w", path, err)
	}
	out := map[string]bool{}
	for i, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !concreteTestName(line) {
			return nil, fmt.Errorf("testmatrix: invalid deferred test at %s:%d: %q", path, i+1, line)
		}
		out[line] = true
	}
	return out, nil
}

func concreteTestName(name string) bool {
	if strings.ContainsAny(name, "<>") {
		return false
	}
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz")
}

func fmtDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// ErrAnyGroupFailed is returned by RunAndReport when at least one group
// reported a non-PASS status.
var ErrAnyGroupFailed = errors.New("testmatrix: one or more groups failed")
