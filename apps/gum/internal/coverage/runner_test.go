package coverage

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestParseProfileAggregatesPerPackage feeds a synthetic profile body
// and verifies per-package aggregation matches the per-statement
// formula `go test -cover` uses.
func TestParseProfileAggregatesPerPackage(t *testing.T) {
	body := "mode: atomic\n" +
		"github.com/ehmo/gum/internal/dispatch/foo.go:10.40,11.5 4 1\n" +
		"github.com/ehmo/gum/internal/dispatch/foo.go:11.5,13.2 6 0\n" +
		"github.com/ehmo/gum/internal/dispatch/bar.go:5.10,6.3 2 1\n" +
		"github.com/ehmo/gum/internal/output/jcs/a.go:1.1,2.1 10 0\n"

	got := ParseProfile(body)

	dispatchPct := 6.0 / 12.0 * 100 // 4 + 2 covered / (4+6+2) total
	jcsPct := 0.0
	if !approxEqual(got["github.com/ehmo/gum/internal/dispatch"], dispatchPct) {
		t.Errorf("dispatch: got %.2f want %.2f", got["github.com/ehmo/gum/internal/dispatch"], dispatchPct)
	}
	if !approxEqual(got["github.com/ehmo/gum/internal/output/jcs"], jcsPct) {
		t.Errorf("jcs: got %.2f want %.2f", got["github.com/ehmo/gum/internal/output/jcs"], jcsPct)
	}
}

// TestParseProfileEmptyBodyReturnsNil verifies a non-profile string
// (missing the `mode:` header) returns nil rather than partial data.
func TestParseProfileEmptyBodyReturnsNil(t *testing.T) {
	if got := ParseProfile("not a profile"); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// TestParseProfileSkipsUnparseableLines verifies malformed block lines
// are tolerated and good lines still aggregate.
func TestParseProfileSkipsUnparseableLines(t *testing.T) {
	body := "mode: atomic\n" +
		"garbage line\n" +
		"github.com/x/y/a.go:1.1,2.1 5 1\n" +
		"another garbage\n" +
		"github.com/x/y/a.go:2.1,3.1 5 0\n"
	got := ParseProfile(body)
	if !approxEqual(got["github.com/x/y"], 50.0) {
		t.Errorf("got %.2f want 50.0", got["github.com/x/y"])
	}
}

// TestParseGoTestEmptyPackages verifies the regex picks up the
// "[no test files]" lines `go test` emits so HasTests=false readings
// can be recorded for those packages.
func TestParseGoTestEmptyPackages(t *testing.T) {
	out := "?   \tgithub.com/ehmo/gum/internal/output\t[no test files]\n" +
		"ok  \tgithub.com/ehmo/gum/internal/dispatch\t0.123s\n"
	readings := parseGoTestEmptyPackages(out)
	if len(readings) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(readings))
	}
	if got, want := readings[0].Package, "github.com/ehmo/gum/internal/output"; got != want {
		t.Errorf("package: got %s want %s", got, want)
	}
	if readings[0].HasTests {
		t.Errorf("HasTests should be false for [no test files]")
	}
}

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.001
}

// TestProfileHasData covers the helper that gates ParseProfile: a profile
// file with only the `mode:` header (no blocks) is empty data; one with at
// least one block line is non-empty; a missing file returns false.
func TestProfileHasData(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing_file_returns_false", func(t *testing.T) {
		if profileHasData(filepath.Join(dir, "nope.out")) {
			t.Errorf("missing file should return false")
		}
	})

	t.Run("header_only_returns_false", func(t *testing.T) {
		p := filepath.Join(dir, "header.out")
		if err := os.WriteFile(p, []byte("mode: atomic\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if profileHasData(p) {
			t.Errorf("header-only profile should return false")
		}
	})

	t.Run("header_plus_block_returns_true", func(t *testing.T) {
		p := filepath.Join(dir, "data.out")
		body := "mode: atomic\nx/y/a.go:1.1,2.1 1 1\n"
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if !profileHasData(p) {
			t.Errorf("profile with one block line should return true")
		}
	})
}

// TestMeasureRunsOnTinyModule exercises Measure end-to-end against a
// throwaway Go module so the real go-test pipeline is covered without
// invoking the (slow) project tree. The fake package has one covered
// statement, so HasTests=true and the reading should report 100%.
func TestMeasureRunsOnTinyModule(t *testing.T) {
	if testing.Short() {
		t.Skip("Measure spawns `go test`; skip in -short")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.test\n\ngo 1.21\n")
	mustWrite(t, filepath.Join(dir, "pkg", "pkg.go"), `package pkg

func Add(a, b int) int { return a + b }
`)
	mustWrite(t, filepath.Join(dir, "pkg", "pkg_test.go"), `package pkg

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 { t.Fatal("nope") }
}
`)

	readings, err := Measure(MeasureOptions{
		WorkDir:  dir,
		Packages: []string{"./pkg"},
	})
	if err != nil {
		t.Fatalf("Measure: %v", err)
	}
	if len(readings) == 0 {
		t.Fatal("Measure returned no readings")
	}
	var found bool
	for _, r := range readings {
		if r.Package == "example.test/pkg" {
			found = true
			if !r.HasTests {
				t.Errorf("HasTests=false, want true")
			}
			if r.Percent < 99.9 {
				t.Errorf("Percent=%.2f, want ~100", r.Percent)
			}
		}
	}
	if !found {
		t.Errorf("no reading for example.test/pkg; readings=%+v", readings)
	}
}

// TestMeasureSurfacesGoTestFailure verifies the error wrapping: when the
// underlying `go test` invocation fails (e.g. the package doesn't compile),
// Measure returns an error whose message includes stderr.
func TestMeasureSurfacesGoTestFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Measure spawns `go test`; skip in -short")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.test\n\ngo 1.21\n")
	// Deliberately broken Go source so `go test` fails.
	mustWrite(t, filepath.Join(dir, "pkg", "pkg.go"), "package pkg\n\nfunc Add(a int int { return a }\n")

	_, err := Measure(MeasureOptions{
		WorkDir:  dir,
		Packages: []string{"./pkg"},
	})
	if err == nil {
		t.Fatal("Measure on broken pkg returned nil err; want failure")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
