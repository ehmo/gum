package main_test

import (
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
