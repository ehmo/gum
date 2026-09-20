package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes one file under dir, making the parents it needs.
func writeFile(t *testing.T, dir, rel, body string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// matrixDoc returns a one-group matrix naming the given proof artifacts.
func matrixDoc(tests ...string) string {
	var b strings.Builder
	b.WriteString("| Requirement | Proof artifact | Required phase |\n|---|---|---|\n")
	b.WriteString("<!-- Group A: fixture group -->\n")
	for _, name := range tests {
		b.WriteString("| fixture requirement | `" + name + "` | v0.1 CI |\n")
	}
	return b.String()
}

// fixtureModule writes a module whose only test is TestFixturePasses and
// returns its directory.
func fixtureModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/fixture\n\ngo 1.26\n")
	writeFile(t, dir, filepath.Join("pkg", "pkg.go"),
		"package pkg\n\nfunc Double(n int) int { return n * 2 }\n")
	writeFile(t, dir, filepath.Join("pkg", "pkg_test.go"),
		"package pkg\n\nimport \"testing\"\n\nfunc TestFixturePasses(t *testing.T) {\n\tif Double(2) != 4 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n")
	return dir
}

// TestRunListsThePlan pins the -list short circuit. It has to print the
// parsed group and its tests and exit 0 without running `go test`, so the
// matrix can be inspected without paying for a sweep.
func TestRunListsThePlan(t *testing.T) {
	dir := t.TempDir()
	matrix := writeFile(t, dir, "matrix.md", matrixDoc("TestFixturePasses", "TestOther"))

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-list", "-matrix=" + matrix}, &stdout, &stderr); code != 0 {
		t.Fatalf("run=%d; want 0\nstderr:\n%s", code, stderr.String())
	}
	for _, want := range []string{"Group A", "fixture group", "(2 tests)", "TestFixturePasses", "TestOther"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

// TestRunSweepsAPassingMatrix pins the success path end to end: parse the
// matrix, run the group against the fixture module, print the summary
// table, and exit 0.
func TestRunSweepsAPassingMatrix(t *testing.T) {
	mod := fixtureModule(t)
	matrix := writeFile(t, t.TempDir(), "matrix.md", matrixDoc("TestFixturePasses"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"-matrix=" + matrix, "-workdir=" + mod, "-timeout=2m"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run=%d; want 0\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Errorf("stdout missing the PASS row:\n%s", stdout.String())
	}
}

// TestRunReportsAFailedGroup pins the exit-1 path. The matrix expects a
// test the fixture module does not define, so the group cannot prove its
// requirement and the sweep must fail rather than report a clean release.
func TestRunReportsAFailedGroup(t *testing.T) {
	mod := fixtureModule(t)
	matrix := writeFile(t, t.TempDir(), "matrix.md", matrixDoc("TestNeverDefined"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"-matrix=" + matrix, "-workdir=" + mod, "-timeout=2m"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run=%d; want 1\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "one or more groups failed") {
		t.Errorf("stderr missing the failure line:\n%s", stderr.String())
	}
}

// TestRunAcceptsADeferredList pins the -deferred arm. A test named in the
// matrix but listed as out of scope must not fail the sweep.
func TestRunAcceptsADeferredList(t *testing.T) {
	mod := fixtureModule(t)
	aux := t.TempDir()
	matrix := writeFile(t, aux, "matrix.md", matrixDoc("TestFixturePasses", "TestNeverDefined"))
	deferred := writeFile(t, aux, "deferred.txt", "TestNeverDefined\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-matrix=" + matrix, "-workdir=" + mod, "-deferred=" + deferred, "-timeout=2m"},
		&stdout, &stderr)
	if code != 0 {
		t.Fatalf("run=%d; want 0\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
}

// failingWriter rejects every write. It stands in for a closed pipe or a
// full disk on the summary destination.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// TestRunReportsASummaryWriteFailure pins the WriteTable arm. Losing the
// summary means the sweep proved nothing the caller can read, so it has
// to exit 2 rather than report the group outcome it cannot print.
func TestRunReportsASummaryWriteFailure(t *testing.T) {
	mod := fixtureModule(t)
	matrix := writeFile(t, t.TempDir(), "matrix.md", matrixDoc("TestFixturePasses"))

	var stderr bytes.Buffer
	code := run([]string{"-matrix=" + matrix, "-workdir=" + mod, "-timeout=2m"},
		failingWriter{}, &stderr)
	if code != 2 {
		t.Fatalf("run=%d; want 2\nstderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "test-matrix: write summary") {
		t.Errorf("stderr missing the write failure:\n%s", stderr.String())
	}
}

// TestRunRejectsAnUnreadableDeferredList pins the exceptions-parse arm.
func TestRunRejectsAnUnreadableDeferredList(t *testing.T) {
	matrix := writeFile(t, t.TempDir(), "matrix.md", matrixDoc("TestFixturePasses"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"-matrix=" + matrix, "-deferred=/no/such/deferred.txt"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("run=%d; want 2", code)
	}
	if !strings.Contains(stderr.String(), "test-matrix: exceptions") {
		t.Errorf("stderr missing the exceptions failure:\n%s", stderr.String())
	}
}

// TestRunRejectsAMissingMatrix pins the parse arm.
func TestRunRejectsAMissingMatrix(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-matrix=/no/such/path.md"}, &stdout, &stderr); code != 2 {
		t.Errorf("run=%d; want 2", code)
	}
	if !strings.Contains(stderr.String(), "test-matrix: parse") {
		t.Errorf("stderr missing the parse failure:\n%s", stderr.String())
	}
}

// TestRunRejectsAMatrixWithNoGroups pins the empty-plan arm. A matrix that
// parses to nothing would otherwise report a clean sweep having proved
// nothing at all.
func TestRunRejectsAMatrixWithNoGroups(t *testing.T) {
	matrix := writeFile(t, t.TempDir(), "matrix.md", "# Test Matrix\n\nNo table here.\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-matrix=" + matrix}, &stdout, &stderr); code != 2 {
		t.Errorf("run=%d; want 2", code)
	}
	if !strings.Contains(stderr.String(), "no groups parsed") {
		t.Errorf("stderr missing the empty-plan failure:\n%s", stderr.String())
	}
}

// TestRunRejectsUnknownFlag pins the flag-parse arm.
func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-nope"}, &stdout, &stderr); code != 2 {
		t.Errorf("run(-nope)=%d; want 2", code)
	}
	if !strings.Contains(stderr.String(), "-matrix") {
		t.Errorf("stderr missing the usage text:\n%s", stderr.String())
	}
}
