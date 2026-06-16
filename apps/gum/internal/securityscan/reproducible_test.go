package securityscan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestReproducibleBuild builds the gum binary twice with identical flags and
// asserts byte-equality. Required by spec §15 (CGO_ENABLED=0 + -trimpath +
// deterministic ldflags ⇒ reproducible artifact for SLSA provenance).
//
// The test is intentionally hermetic: same source tree, same Go toolchain,
// same environment, two builds. If the binary differs, something non-
// deterministic has leaked into the build (timestamps, paths, random ids).
func TestReproducibleBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping reproducible-build test in -short mode")
	}

	root := moduleRoot(t)
	tmp := t.TempDir()

	// The nested `go build` can transiently fail under the concurrent-
	// compilation load of a full `go test -count=1 ./...` run (gum-qooz): the
	// build cache / linker contend with the outer runner rebuilding every
	// package. That is a load flake, not a reproducibility break — a genuine
	// non-deterministic build still produces two successful builds with
	// differing hashes (caught below), and a real compile error fails on every
	// attempt. So a bounded retry only masks the transient load failure.
	build := func(out string) string {
		t.Helper()
		const maxAttempts = 3
		var lastErr error
		var lastStderr string
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			cmd := exec.Command("go", "build",
				"-trimpath",
				"-ldflags", "-s -w -X main.version=v0.0.0-reproducibility-check",
				"-o", out,
				"./cmd/gum")
			cmd.Dir = root
			cmd.Env = append(cmd.Environ(),
				"CGO_ENABLED=0",
				"GOFLAGS=",
				"SOURCE_DATE_EPOCH=1700000000",
			)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				lastErr, lastStderr = err, stderr.String()
				if attempt < maxAttempts {
					time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
				}
				continue
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			return hex.EncodeToString(sum[:])
		}
		t.Fatalf("build failed after %d attempts: %v\nstderr: %s", maxAttempts, lastErr, lastStderr)
		return ""
	}

	a := build(filepath.Join(tmp, "gum-a"))
	b := build(filepath.Join(tmp, "gum-b"))
	if a != b {
		t.Fatalf("non-reproducible build:\n  build A sha256: %s\n  build B sha256: %s", a, b)
	}
}
