package securityscan

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestReleaseBinaryNoCGo asserts that the production gum binary's dependency
// closure contains no CGo packages. Required by spec §14 (single static
// binary, CGO_ENABLED=0 across the release matrix).
func TestReleaseBinaryNoCGo(t *testing.T) {
	root := moduleRoot(t)

	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}} {{if .CgoFiles}}CGO{{end}}", "./cmd/gum/...")
	cmd.Dir = root
	cmd.Env = append(cmd.Environ(), "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list failed: %v\nstderr: %s", err, stderr.String())
	}

	var leaks []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasSuffix(line, " CGO") {
			leaks = append(leaks, strings.TrimSuffix(line, " CGO"))
		}
	}
	if len(leaks) > 0 {
		t.Fatalf("CGo dependencies detected in ./cmd/gum/... closure:\n  %s", strings.Join(leaks, "\n  "))
	}
}
