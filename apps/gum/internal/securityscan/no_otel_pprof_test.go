package securityscan

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestNoOTelImportV01 asserts that v0.1.0 binaries do not import any
// OpenTelemetry packages. Required by spec §14.1 rule 5 (no OTel in v0.1).
func TestNoOTelImportV01(t *testing.T) {
	leaks := importsMatchingPrefix(t, "go.opentelemetry.io/")
	if len(leaks) > 0 {
		t.Fatalf("OpenTelemetry imports detected in ./cmd/gum/... closure (spec §14.1 rule 5 forbids OTel in v0.1.0):\n  %s",
			strings.Join(leaks, "\n  "))
	}
}

// TestNoPprofImportV01 asserts that v0.1.0 binaries do not import
// net/http/pprof or runtime/pprof's HTTP-exposing facilities. Required by
// spec §14.1 rule 6 (no pprof HTTP surface in v0.1).
func TestNoPprofImportV01(t *testing.T) {
	leaks := importsMatchingPrefix(t, "net/http/pprof")
	if len(leaks) > 0 {
		t.Fatalf("pprof imports detected in ./cmd/gum/... closure (spec §14.1 rule 6 forbids pprof in v0.1.0):\n  %s",
			strings.Join(leaks, "\n  "))
	}
}

func importsMatchingPrefix(t *testing.T, prefix string) []string {
	t.Helper()
	root := moduleRoot(t)

	cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", "./cmd/gum/...")
	cmd.Dir = root
	cmd.Env = append(cmd.Environ(), "CGO_ENABLED=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list failed: %v\nstderr: %s", err, stderr.String())
	}

	var matches []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, prefix) {
			matches = append(matches, line)
		}
	}
	return matches
}
