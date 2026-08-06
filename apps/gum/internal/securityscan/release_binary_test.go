package securityscan

import (
	"strings"
	"testing"
)

// TestReleaseBinaryNoCGo asserts that the production gum binary's dependency
// closure contains no CGo packages. Required by spec §14 (single static
// binary, CGO_ENABLED=0 across the release matrix).
func TestReleaseBinaryNoCGo(t *testing.T) {
	out := runGo(t, []string{"CGO_ENABLED=0"},
		"list", "-deps", "-f", "{{.ImportPath}} {{if .CgoFiles}}CGO{{end}}", "./cmd/gum/...")

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
