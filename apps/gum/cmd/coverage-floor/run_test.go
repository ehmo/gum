package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writerFor returns a helper that writes one file into mod, making any
// parent directories the fixture needs.
func writerFor(t *testing.T, mod string) func(rel, body string) {
	t.Helper()
	return func(rel, body string) {
		path := filepath.Join(mod, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fixtureModule writes a throwaway module whose ./internal/... package
// reads 0% so run() takes the violation path, and returns its dir.
func fixtureModule(t *testing.T) string {
	t.Helper()
	mod := t.TempDir()
	write := writerFor(t, mod)
	write("go.mod", "module example.com/low\n\ngo 1.26\n")
	write(filepath.Join("cmd", "gum", "main.go"), "package main\n\nfunc main() {}\n")
	write(filepath.Join("internal", "low", "low.go"),
		"package low\n\nfunc Double(n int) int { return n * 2 }\n")
	write(filepath.Join("internal", "low", "low_test.go"),
		"package low\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n")
	return mod
}

// TestRunReportsViolation drives the whole command in-process against a
// fixture module. The exit code splits on GOOS: every ratchet was
// measured on linux, so only a linux reading may fail the run.
func TestRunReportsViolation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-workdir=" + fixtureModule(t)}, &stdout, &stderr)

	if !strings.Contains(stderr.String(), "example.com/low/internal/low: 0.0% < 85.0%") {
		t.Fatalf("stderr missing the violation:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "PACKAGE") {
		t.Errorf("stdout missing the table header:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "measured on GOOS="+runtime.GOOS) {
		t.Errorf("stdout missing the platform line:\n%s", stdout.String())
	}

	want := 0
	if runtime.GOOS == "linux" {
		want = 1
	}
	if code != want {
		t.Errorf("run=%d; want %d on GOOS=%s", code, want, runtime.GOOS)
	}
}

// TestRunCleanModuleExitsZero pins the no-violation return. The gated
// package in the fixture is fully covered, so the table prints an "ok"
// row and the command reports success on any platform.
func TestRunCleanModuleExitsZero(t *testing.T) {
	mod := t.TempDir()
	write := writerFor(t, mod)
	write("go.mod", "module example.com/clean\n\ngo 1.26\n")
	write(filepath.Join("cmd", "gum", "main.go"), "package main\n\nfunc main() {}\n")
	write(filepath.Join("internal", "hi", "hi.go"),
		"package hi\n\nfunc Double(n int) int { return n * 2 }\n")
	write(filepath.Join("internal", "hi", "hi_test.go"),
		"package hi\n\nimport \"testing\"\n\nfunc TestDouble(t *testing.T) {\n\tif Double(2) != 4 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-workdir=" + mod}, &stdout, &stderr); code != 0 {
		t.Fatalf("run=%d; want 0\nstderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "example.com/clean/internal/hi") {
		t.Errorf("stdout missing the covered package row:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "ok") {
		t.Errorf("stdout missing the ok mark:\n%s", stdout.String())
	}
}

// TestRunPrintsTheNoTestsRow pins the HasTests=false row. Under
// -coverprofile `go test` only prints "[no test files]" when no package
// in the run reports a coverage percentage, so every package in the
// fixture has to be free of instrumentable statements. Such a package
// carries no reading to compare, and the table has to say so rather
// than print it as 0% and fail the floor.
func TestRunPrintsTheNoTestsRow(t *testing.T) {
	mod := t.TempDir()
	write := writerFor(t, mod)
	write("go.mod", "module example.com/empty\n\ngo 1.26\n")
	write(filepath.Join("cmd", "gum", "main.go"), "package main\n\nfunc main() {}\n")
	write(filepath.Join("internal", "bare", "bare.go"),
		"package bare\n\ntype T struct{ N int }\n\nconst X = 1\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-workdir=" + mod}, &stdout, &stderr); code != 0 {
		t.Fatalf("run=%d; want 0\nstderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "n/a (no tests)") {
		t.Errorf("stdout missing the no-tests row:\n%s", stdout.String())
	}
}

// TestRunPrintsTheRatchetHint pins the warn-only opportunity block. The
// fixture borrows gum's own module path so one of its packages matches a
// real Ratchets entry, and covers it fully so the reading clears
// Min+RatchetOpportunityMargin. The hint is suppressed off the baseline
// platform, so only its absence is assertable on darwin.
func TestRunPrintsTheRatchetHint(t *testing.T) {
	mod := t.TempDir()
	write := writerFor(t, mod)
	write("go.mod", "module github.com/ehmo/gum\n\ngo 1.26\n")
	write(filepath.Join("cmd", "gum", "main.go"), "package main\n\nfunc main() {}\n")
	write(filepath.Join("internal", "dispatch", "dispatch.go"),
		"package dispatch\n\nfunc Double(n int) int { return n * 2 }\n")
	write(filepath.Join("internal", "dispatch", "dispatch_test.go"),
		"package dispatch\n\nimport \"testing\"\n\nfunc TestDouble(t *testing.T) {\n\tif Double(2) != 4 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n")

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-workdir=" + mod}, &stdout, &stderr); code != 0 {
		t.Fatalf("run=%d; want 0\nstderr:\n%s", code, stderr.String())
	}

	got := strings.Contains(stdout.String(), "RATCHET_OPPORTUNITY")
	want := runtime.GOOS == "linux"
	if got != want {
		t.Errorf("RATCHET_OPPORTUNITY present=%v; want %v on GOOS=%s\n%s",
			got, want, runtime.GOOS, stdout.String())
	}
}

// TestRunRejectsUnknownFlag pins the flag-parse arm. An unusable
// invocation must exit 2 rather than measure anything.
func TestRunRejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-nope"}, &stdout, &stderr); code != 2 {
		t.Errorf("run(-nope)=%d; want 2", code)
	}
	if !strings.Contains(stderr.String(), "-workdir") {
		t.Errorf("stderr missing the usage text:\n%s", stderr.String())
	}
}

// TestRunRejectsUnmeasurableModule pins the measure-failure arm.
// -workdir names a directory with no go.mod, so `go test` cannot run.
func TestRunRejectsUnmeasurableModule(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-workdir=" + t.TempDir()}, &stdout, &stderr); code != 2 {
		t.Errorf("run(no-module)=%d; want 2", code)
	}
	if !strings.Contains(stderr.String(), "coverage-floor: measure:") {
		t.Errorf("stderr missing the measure failure:\n%s", stderr.String())
	}
}
