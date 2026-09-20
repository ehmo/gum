package testmatrix

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempModule creates a throwaway Go module holding a single passing
// test, and returns its root. realGoTest shells out to the real `go test`,
// so it needs a real module to run against; a one-file module keeps the
// compile cheap.
func writeTempModule(t *testing.T, testName string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module testmatrixprobe\n\ngo 1.26\n",
		"probe_test.go": "package probe\n\nimport \"testing\"\n\nfunc " + testName +
			"(t *testing.T) {}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// TestRunGroupFallsBackToRealGoTest covers the `hook == nil → realGoTest`
// default (runner.go:76-78) and realGoTest itself (runner.go:128-136).
// Every other runner test injects RunHook, so the shipped default and the
// exec.CommandContext it builds are otherwise never executed.
func TestRunGroupFallsBackToRealGoTest(t *testing.T) {
	dir := writeTempModule(t, "TestTestmatrixProbe")
	r := &Runner{WorkDir: dir}
	results := r.RunAll(context.Background(), []Group{{
		Letter:      "A",
		Description: "real go test",
		Tests:       []string{"TestTestmatrixProbe"},
	}})

	if len(results) != 1 {
		t.Fatalf("RunAll returned %d results; want 1", len(results))
	}
	got := results[0]
	if got.Status != StatusPassed {
		t.Fatalf("Status=%q stdout=%q stderr=%q; want PASS", got.Status, got.Stdout, got.Stderr)
	}
	if got.TestsRun != 1 {
		t.Errorf("TestsRun=%d; want 1", got.TestsRun)
	}
	if len(got.MissingTests) != 0 {
		t.Errorf("MissingTests=%v; want none", got.MissingTests)
	}
}

// TestRunGroupReportsRealGoTestFailure pins realGoTest's non-nil error
// return. The group names a test the probe module does not define, so
// `go test` runs nothing and the group is reported as missing.
func TestRunGroupReportsRealGoTestFailure(t *testing.T) {
	dir := writeTempModule(t, "TestTestmatrixProbe")
	r := &Runner{WorkDir: dir}
	results := r.RunAll(context.Background(), []Group{{
		Letter: "B",
		Tests:  []string{"TestTestmatrixAbsent"},
	}})

	if results[0].Status != StatusFailed {
		t.Fatalf("Status=%q; want FAIL", results[0].Status)
	}
	if want := []string{"TestTestmatrixAbsent"}; len(results[0].MissingTests) != 1 ||
		results[0].MissingTests[0] != want[0] {
		t.Errorf("MissingTests=%v; want %v", results[0].MissingTests, want)
	}
}

// errReader fails every Read. Parse wraps a bufio.Scanner, and the only way
// into its scanner.Err() arm is a reader that reports a non-EOF failure.
type errReader struct{}

var errRead = errors.New("synthetic read failure")

func (errReader) Read([]byte) (int, error) { return 0, errRead }

// TestParseSurfacesScannerError covers parser.go:90-92.
func TestParseSurfacesScannerError(t *testing.T) {
	groups, err := Parse(errReader{})
	if !errors.Is(err, errRead) {
		t.Fatalf("Parse(errReader) err=%v; want errRead", err)
	}
	if groups != nil {
		t.Errorf("Parse(errReader) groups=%v; want nil", groups)
	}
	if !strings.Contains(err.Error(), "testmatrix: scan") {
		t.Errorf("err=%q; want it to name the scan step", err)
	}
}

// TestParseDeferredFileMissingPathWraps covers runner.go:282-285. The other
// ParseDeferredFile tests all pass a file that exists.
func TestParseDeferredFileMissingPathWraps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.txt")
	got, err := ParseDeferredFile(path)
	if err == nil {
		t.Fatal("ParseDeferredFile(absent) err=nil; want open failure")
	}
	if got != nil {
		t.Errorf("ParseDeferredFile(absent) map=%v; want nil", got)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err=%v; want it to wrap os.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("err=%q; want it to name %s", err, path)
	}
}

// TestWriteTableDeferredWriteErrorPropagates covers runner.go:265-268. The
// deferred line is only attempted when a result carries documented
// out-of-scope tests, which no other WriteTable test sets.
func TestWriteTableDeferredWriteErrorPropagates(t *testing.T) {
	w := &failingWriter{failOn: 4}
	s := Summarize([]Result{{
		Group:    Group{Letter: "A"},
		Status:   StatusPassed,
		Deferred: []string{"TestDeferredX"},
	}})
	if err := s.WriteTable(w); !errors.Is(err, errFail) {
		t.Errorf("WriteTable(deferred, failOn=4) err=%v; want errFail", err)
	}
}
