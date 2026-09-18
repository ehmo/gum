package main_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCmdHelpFlag verifies -help lists the documented flag set
// without spawning any `go test` invocations.
func TestCmdHelpFlag(t *testing.T) {
	bin := buildBinary(t)
	cmd := exec.Command(bin, "-help")
	out, _ := cmd.CombinedOutput()
	body := string(out)
	if !strings.Contains(body, "-workdir") {
		t.Errorf("help output missing -workdir flag:\n%s", body)
	}
}

// TestViolationFailsOnlyOnBaseline pins the platform split. Every ratchet was
// measured on linux, so a floor violation fails the run there and is only a
// warning on any other GOOS. The module under test holds one package with an
// untested function, which reads 0% against the 85% floor.
func TestViolationFailsOnlyOnBaseline(t *testing.T) {
	bin := buildBinary(t)
	mod := t.TempDir()
	writeFile(t, filepath.Join(mod, "go.mod"), "module example.com/low\n\ngo 1.26\n")
	writeFile(t, filepath.Join(mod, "cmd", "gum", "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(mod, "internal", "low", "low.go"), "package low\n\nfunc Double(n int) int { return n * 2 }\n")
	writeFile(t, filepath.Join(mod, "internal", "low", "low_test.go"), "package low\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n")

	cmd := exec.Command(bin, "-workdir="+mod)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	if !strings.Contains(stderr.String(), "example.com/low/internal/low: 0.0% < 85.0%") {
		t.Fatalf("stderr missing the violation:\n%s", stderr.String())
	}

	var exitErr *exec.ExitError
	if runtime.GOOS == "linux" {
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("on linux: err = %v, want exit status 1\nstderr:\n%s", err, stderr.String())
		}
		return
	}

	if err != nil {
		t.Fatalf("on %s: err = %v, want exit status 0\nstderr:\n%s", runtime.GOOS, err, stderr.String())
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "coverage-floor")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = sourceDir(t)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build cmd/coverage-floor: %v", err)
	}
	return bin
}

func sourceDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Dir(thisFile)
}
