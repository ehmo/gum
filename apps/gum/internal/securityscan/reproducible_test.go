package securityscan

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
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

	tmp := t.TempDir()

	// runGo carries the bounded retry that absorbs the nested-build load flake
	// (gum-qooz); see toolchainAttempts for why retrying cannot hide a real
	// reproducibility break.
	build := func(out string) string {
		t.Helper()
		runGo(t, []string{"CGO_ENABLED=0", "GOFLAGS=", "SOURCE_DATE_EPOCH=1700000000"},
			"build", "-trimpath",
			"-ldflags", "-s -w -X main.version=v0.0.0-reproducibility-check",
			"-o", out, "./cmd/gum")
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}

	a := build(filepath.Join(tmp, "gum-a"))
	b := build(filepath.Join(tmp, "gum-b"))
	if a != b {
		t.Fatalf("non-reproducible build:\n  build A sha256: %s\n  build B sha256: %s", a, b)
	}
}
