package profile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// runOneFixture is exercised via profile.RunFixtures with a single-test
// profile. Each subtest pins one expectation-failure or guard branch so
// regressions that swallow a mismatch (or fail the wrong way) surface
// here as a concrete "missing failure" / "wrong failure" diff.

func TestRunFixturesEmptyFixturePathFails(t *testing.T) {
	p, err := profile.Parse("default_format = \"toon\"\n\n[[tests]]\nname = \"no path\"\nfixture = \"\"\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, "testdata")
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if res.Passed {
		t.Fatal("Passed=true; want false (empty fixture path)")
	}
	if len(res.Fixtures) != 1 {
		t.Fatalf("len(Fixtures)=%d; want 1", len(res.Fixtures))
	}
	if !containsAny(res.Fixtures[0].Failures, "fixture path is empty") {
		t.Errorf("failures=%v; want 'fixture path is empty'", res.Fixtures[0].Failures)
	}
}

func TestRunFixturesReadErrorFails(t *testing.T) {
	p, err := profile.Parse("default_format = \"toon\"\n\n[[tests]]\nname = \"missing\"\nfixture = \"does-not-exist.json\"\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, t.TempDir())
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if res.Passed {
		t.Fatal("Passed=true; want false (fixture missing)")
	}
	if !containsAny(res.Fixtures[0].Failures, "read fixture") {
		t.Errorf("failures=%v; want 'read fixture' wrap", res.Fixtures[0].Failures)
	}
}

func TestRunFixturesAbsoluteFixturePathRespected(t *testing.T) {
	tmp := t.TempDir()
	abs := filepath.Join(tmp, "abs.json")
	if err := os.WriteFile(abs, []byte(`[{"a":1}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	src := "default_format = \"toon\"\n\n[[tests]]\nname = \"abs\"\nfixture = " + quote(abs) + "\nexpect_format = \"toon\"\n"
	p, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// baseDir is empty on purpose: absolute path must NOT be joined.
	res, err := profile.RunFixtures(p, "")
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if !res.Passed {
		t.Fatalf("Passed=false; failures=%+v", res.Fixtures[0].Failures)
	}
}

func TestRunFixturesExpectFormatMismatchFails(t *testing.T) {
	tmp := t.TempDir()
	fix := filepath.Join(tmp, "f.json")
	if err := os.WriteFile(fix, []byte(`[{"a":1}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `default_format = "toon"

[[tests]]
name = "fmt"
fixture = "f.json"
expect_format = "json"
`
	p, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, tmp)
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if res.Passed {
		t.Fatal("Passed=true; want false (format mismatch)")
	}
	if !containsAny(res.Fixtures[0].Failures, "expect_format") {
		t.Errorf("failures=%v; want 'expect_format' diag", res.Fixtures[0].Failures)
	}
}

func TestRunFixturesExpectLossyMismatchFails(t *testing.T) {
	tmp := t.TempDir()
	fix := filepath.Join(tmp, "f.json")
	if err := os.WriteFile(fix, []byte(`[{"a":1}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Profile has no projection/limit/etc. → ActualLossy=false. Expect true → mismatch.
	src := `default_format = "toon"

[[tests]]
name = "lossy"
fixture = "f.json"
expect_lossy = true
`
	p, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, tmp)
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if res.Passed {
		t.Fatal("Passed=true; want false (lossy mismatch)")
	}
	if !containsAny(res.Fixtures[0].Failures, "expect_lossy") {
		t.Errorf("failures=%v; want 'expect_lossy' diag", res.Fixtures[0].Failures)
	}
}

func TestRunFixturesExpectResultCountMismatchFails(t *testing.T) {
	tmp := t.TempDir()
	fix := filepath.Join(tmp, "f.json")
	if err := os.WriteFile(fix, []byte(`[{"a":1},{"a":2}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `default_format = "toon"

[[tests]]
name = "rc"
fixture = "f.json"
expect_result_count = 99
`
	p, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, tmp)
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if res.Passed {
		t.Fatal("Passed=true; want false (result-count mismatch)")
	}
	if !containsAny(res.Fixtures[0].Failures, "expect_result_count") {
		t.Errorf("failures=%v; want 'expect_result_count' diag", res.Fixtures[0].Failures)
	}
}

func TestRunFixturesExpectOmittedCountMismatchFails(t *testing.T) {
	tmp := t.TempDir()
	fix := filepath.Join(tmp, "f.json")
	if err := os.WriteFile(fix, []byte(`{"items":[1,2],"omitted_count":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `default_format = "toon"

[[tests]]
name = "oc"
fixture = "f.json"
expect_omitted_count = 99
`
	p, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, tmp)
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if res.Passed {
		t.Fatal("Passed=true; want false (omitted-count mismatch)")
	}
	if !containsAny(res.Fixtures[0].Failures, "expect_omitted_count") {
		t.Errorf("failures=%v; want 'expect_omitted_count' diag", res.Fixtures[0].Failures)
	}
}

func TestRunFixturesExpectFieldsMissingFails(t *testing.T) {
	tmp := t.TempDir()
	fix := filepath.Join(tmp, "f.json")
	if err := os.WriteFile(fix, []byte(`[{"a":1}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	src := `default_format = "toon"

[[tests]]
name = "fields"
fixture = "f.json"
expect_fields = ["nonexistent_field_xyz"]
`
	p, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, tmp)
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if res.Passed {
		t.Fatal("Passed=true; want false (expect_fields missing)")
	}
	if !containsAny(res.Fixtures[0].Failures, "expect_fields") {
		t.Errorf("failures=%v; want 'expect_fields' diag", res.Fixtures[0].Failures)
	}
}

func containsAny(failures []string, sub string) bool {
	for _, f := range failures {
		if strings.Contains(f, sub) {
			return true
		}
	}
	return false
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
