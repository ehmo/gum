// Package golden is the project-wide snapshot/golden-file helper for tests
// that need to lock down byte-for-byte output (TOON renderings, JSON envelope
// shapes, output-schema fixtures, etc.).
//
// Decision (gum-b22o.2): hand-rolled rather than github.com/sebdah/goldie.
// Rationale:
//
//   - The existing codebase already had three independent hand-rolled patterns
//     (internal/output/profile/profile_test.go, internal/output/toon/encoder_test.go,
//     internal/help/searchapis_test.go). Replacing them with a third-party
//     dependency that adds a supply-chain surface for ~40 lines of code is a
//     bad trade.
//   - The project explicitly favors minimal go.mod surface (see go.mod
//     comment: only one assertion / fuzz / canonical-JSON dep each).
//   - The full feature set we need is small: read-or-write a golden file,
//     honor a CLI flag to regenerate, and emit a clear diff on mismatch.
//
// Usage:
//
//	import "github.com/ehmo/gum/internal/testutil/golden"
//
//	func TestSomething(t *testing.T) {
//	    got := renderSomething()
//	    golden.Bytes(t, "testdata/golden/toon/something.toon", got)
//	}
//
// To regenerate every golden file affected by a test run:
//
//	go test ./... -update
//
// The -update flag is registered package-locally via init(); tests importing
// this package automatically pick it up without having to register the flag
// themselves.
package golden

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false,
	"regenerate golden files in testdata/golden/ from current test output")

// Bytes compares got against the file at path. If the file does not exist or
// -update was passed, Bytes writes got to path (creating parent directories
// mode 0o755 and the file mode 0o644). Otherwise, on mismatch, Bytes calls
// t.Fatalf with a unified-ish diff trimmed to the first 40 differing lines.
//
// path is interpreted relative to the test's working directory, matching the
// convention of every other helper in this codebase. Tests that need a
// runtime.Caller-based testdata anchor can construct the absolute path and
// pass it in.
func Bytes(t testing.TB, path string, got []byte) {
	t.Helper()

	if *update {
		writeGolden(t, path, got)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("golden: read %s: %v", path, err)
		}
		t.Logf("golden: %s does not exist; writing initial fixture (re-run with -update to suppress)", path)
		writeGolden(t, path, got)
		return
	}

	if bytes.Equal(want, got) {
		return
	}
	t.Fatalf("golden mismatch for %s\nrun: go test -update %s\n%s",
		path, packageHint(path), unifiedDiff(string(want), string(got), 40))
}

// String is the string-typed convenience over Bytes.
func String(t testing.TB, path, got string) {
	t.Helper()
	Bytes(t, path, []byte(got))
}

func writeGolden(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("golden: mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("golden: write %s: %v", path, err)
	}
}

// unifiedDiff returns a compact line-by-line diff of want vs got, capped at
// maxLines differing rows so a 10k-line mismatch doesn't drown the test log.
// Format: "- " prefix for want-only lines, "+ " prefix for got-only lines,
// "  " prefix for equal-context lines on either side.
func unifiedDiff(want, got string, maxLines int) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	var b strings.Builder
	b.WriteString("--- want\n+++ got\n")
	n := len(w)
	if len(g) > n {
		n = len(g)
	}
	differing := 0
	for i := 0; i < n; i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl == gl {
			continue
		}
		differing++
		if differing > maxLines {
			b.WriteString("... diff truncated\n")
			break
		}
		if i < len(w) {
			b.WriteString("- ")
			b.WriteString(wl)
			b.WriteByte('\n')
		}
		if i < len(g) {
			b.WriteString("+ ")
			b.WriteString(gl)
			b.WriteByte('\n')
		}
	}
	if differing == 0 {
		b.WriteString("(line-count differs but content is byte-identical per line; trailing newline mismatch?)\n")
	}
	return b.String()
}

// packageHint returns the directory hint shown in the regenerate-command
// suggestion in the test failure message, e.g. "./internal/output/toon/...".
// It strips the path to the first "internal/" or "cmd/" segment so the user
// can copy-paste the go test command.
func packageHint(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	for _, marker := range []string{"/internal/", "/cmd/"} {
		idx := strings.Index(abs, marker)
		if idx == -1 {
			continue
		}
		rel := abs[idx+1:] // drop leading slash
		dir := filepath.Dir(rel)
		return "./" + dir + "/..."
	}
	return "./..."
}
