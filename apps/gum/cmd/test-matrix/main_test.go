package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCmdNonZeroExitOnMissingMatrix verifies the binary exits non-zero
// (exit-code 2) when -matrix points at a path that does not exist. This
// is the most reliable end-to-end smoke test that does not require
// spawning a real `go test` recursion.
func TestCmdNonZeroExitOnMissingMatrix(t *testing.T) {
	bin := buildTestMatrix(t)
	cmd := exec.Command(bin, "-matrix=/no/such/path.md", "-workdir=.")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, got success; output:\n%s", out)
	}
	if !strings.Contains(string(out), "parse") {
		t.Errorf("output should reference parse failure, got:\n%s", out)
	}
}

// TestCmdHelpFlag verifies -help returns the documented flag set
// without spawning any go test invocations.
func TestCmdHelpFlag(t *testing.T) {
	bin := buildTestMatrix(t)
	cmd := exec.Command(bin, "-help")
	out, _ := cmd.CombinedOutput() // -help exits 2 from flag package; ignore err
	body := string(out)
	for _, want := range []string{"-matrix", "-workdir", "-timeout"} {
		if !strings.Contains(body, want) {
			t.Errorf("help output missing flag %q\n%s", want, body)
		}
	}
}

// buildTestMatrix compiles the binary into a temp directory and returns
// the absolute path. Recompiling per test keeps the smoke tests
// hermetic against stale binaries on disk.
func buildTestMatrix(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "test-matrix")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = sourceDir(t)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build cmd/test-matrix: %v", err)
	}
	return bin
}

func sourceDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Dir(thisFile)
}
