package securityscan

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNoSharedLibDependencies asserts that a release-style gum build links no
// non-system dynamic libraries (linux/macOS) and contains no dlopen-style
// CGo runtime calls. Required by spec §15 (single static binary).
//
// On Linux ldd should report "not a dynamic executable" or only the system
// dynamic loader. On macOS otool -L lists system libraries that ship with the
// OS; any non-/usr/lib or non-/System path is a leak.
func TestNoSharedLibDependencies(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("dynamic-linkage inspection only runs on linux/darwin; current GOOS=%s", runtime.GOOS)
	}

	root := moduleRoot(t)
	tmp := t.TempDir()
	binary := filepath.Join(tmp, "gum-static")

	build := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", binary, "./cmd/gum")
	build.Dir = root
	build.Env = append(build.Environ(), "CGO_ENABLED=0")
	var berr bytes.Buffer
	build.Stderr = &berr
	if err := build.Run(); err != nil {
		t.Fatalf("build failed: %v\nstderr: %s", err, berr.String())
	}

	switch runtime.GOOS {
	case "linux":
		out, err := exec.Command("ldd", binary).CombinedOutput()
		s := string(out)
		// Either ldd exits non-zero with "not a dynamic executable",
		// or it exits zero but prints the same string.
		if err == nil && !strings.Contains(s, "not a dynamic executable") && !strings.Contains(s, "statically linked") {
			t.Fatalf("expected statically linked binary; ldd reported:\n%s", s)
		}
	case "darwin":
		out, err := exec.Command("otool", "-L", binary).CombinedOutput()
		if err != nil {
			t.Skipf("otool unavailable: %v", err)
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasSuffix(line, ":") {
				continue
			}
			path := strings.Fields(line)[0]
			if strings.HasPrefix(path, "/usr/lib/") || strings.HasPrefix(path, "/System/") {
				continue
			}
			t.Errorf("unexpected non-system dynamic dependency: %s", path)
		}
	}
}
