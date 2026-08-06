package securityscan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionStamp verifies that -ldflags '-X main.version=...' propagates to
// the `gum version` and `gum --version` output. Required by spec §14 + §15
// (release binaries must carry the git tag string).
func TestVersionStamp(t *testing.T) {
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "gum-test")
	if isWindows() {
		binary += ".exe"
	}

	const stamp = "v0.0.0-test-stamp"
	runGo(t, []string{"CGO_ENABLED=0"},
		"build", "-ldflags", "-X main.version="+stamp, "-o", binary, "./cmd/gum")

	for _, args := range [][]string{{"version"}, {"--version"}} {
		out, err := exec.Command(binary, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("gum %v: %v\noutput: %s", args, err, out)
		}
		if !strings.Contains(string(out), stamp) {
			t.Errorf("gum %v: expected version %q in output, got %q", args, stamp, strings.TrimSpace(string(out)))
		}
	}
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}
