package profile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// TestParserAcceptsTestsBlock verifies that the parser recognizes [[tests]]
// section headers and accumulates fields into Profile.Tests (spec §12.1,
// docs/expression-profile-dsl.md "Test Format").
func TestParserAcceptsTestsBlock(t *testing.T) {
	src := `default_format = "toon"
limit = 2

[[tests]]
name = "smoke"
fixture = "smoke.json"
expect_format = "toon"
expect_max_tokens = 500
expect_lossy = true
expect_result_count = 2
expect_omitted_count = 1
expect_fields = ["id", "name"]
`
	p, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Tests) != 1 {
		t.Fatalf("len(Tests) = %d; want 1", len(p.Tests))
	}
	tc := p.Tests[0]
	if tc.Name != "smoke" {
		t.Errorf("Tests[0].Name = %q; want %q", tc.Name, "smoke")
	}
	if tc.Fixture != "smoke.json" {
		t.Errorf("Tests[0].Fixture = %q; want %q", tc.Fixture, "smoke.json")
	}
	if tc.ExpectFormat != "toon" {
		t.Errorf("Tests[0].ExpectFormat = %q; want %q", tc.ExpectFormat, "toon")
	}
	if tc.ExpectMaxTokens != 500 {
		t.Errorf("Tests[0].ExpectMaxTokens = %d; want 500", tc.ExpectMaxTokens)
	}
	if !tc.ExpectLossySet || !tc.ExpectLossy {
		t.Errorf("Tests[0].ExpectLossy = (%v, set=%v); want (true, true)", tc.ExpectLossy, tc.ExpectLossySet)
	}
	if !tc.ExpectResultCountSet || tc.ExpectResultCount != 2 {
		t.Errorf("Tests[0].ExpectResultCount = (%d, set=%v); want (2, true)", tc.ExpectResultCount, tc.ExpectResultCountSet)
	}
	if !tc.ExpectOmittedCountSet || tc.ExpectOmittedCount != 1 {
		t.Errorf("Tests[0].ExpectOmittedCount = (%d, set=%v); want (1, true)", tc.ExpectOmittedCount, tc.ExpectOmittedCountSet)
	}
	if len(tc.ExpectFields) != 2 || tc.ExpectFields[0] != "id" || tc.ExpectFields[1] != "name" {
		t.Errorf("Tests[0].ExpectFields = %v; want [id, name]", tc.ExpectFields)
	}
}

// TestParserAcceptsMultipleTestsBlocks verifies that multiple [[tests]] blocks
// accumulate into Profile.Tests in declaration order.
func TestParserAcceptsMultipleTestsBlocks(t *testing.T) {
	src := `default_format = "toon"

[[tests]]
name = "first"
fixture = "a.json"

[[tests]]
name = "second"
fixture = "b.json"
`
	p, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Tests) != 2 {
		t.Fatalf("len(Tests) = %d; want 2", len(p.Tests))
	}
	if p.Tests[0].Name != "first" || p.Tests[1].Name != "second" {
		t.Errorf("Tests order: %v / %v; want first, second", p.Tests[0].Name, p.Tests[1].Name)
	}
}

// TestParserRejectsUnknownTestsKey verifies that unknown keys inside [[tests]]
// blocks are rejected to match the strict-DSL contract.
func TestParserRejectsUnknownTestsKey(t *testing.T) {
	src := `default_format = "toon"

[[tests]]
name = "x"
expect_galaxy = 7
`
	_, err := profile.Parse(src)
	if err == nil {
		t.Fatal("Parse: expected error for unknown tests key, got nil")
	}
	if !strings.Contains(err.Error(), "tests.expect_galaxy") {
		t.Errorf("error should mention 'tests.expect_galaxy', got: %v", err)
	}
}

// TestParserRejectsUnknownSectionHeader verifies that section headers other
// than [[tests]] are rejected (no silent acceptance of mistyped tables).
func TestParserRejectsUnknownSectionHeader(t *testing.T) {
	src := `default_format = "toon"
[unknown_section]
key = "value"
`
	_, err := profile.Parse(src)
	if err == nil {
		t.Fatal("Parse: expected error for unknown section header, got nil")
	}
}

// TestRunFixturesGmailList drives a real profile + fixture round-trip and
// asserts the resulting FixtureRunResult passes with the expected token/
// result-count values.
func TestRunFixturesGmailList(t *testing.T) {
	baseDir := "testdata"
	profileSrc, err := readFile(t, filepath.Join(baseDir, "gmail-list-with-tests.toml"))
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	p, err := profile.Parse(profileSrc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, baseDir)
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if !res.Passed {
		t.Fatalf("RunFixtures.Passed = false; fixtures: %+v", res.Fixtures)
	}
	if len(res.Fixtures) != 1 {
		t.Fatalf("len(Fixtures) = %d; want 1", len(res.Fixtures))
	}
	r := res.Fixtures[0]
	if r.Name != "gmail list compact" {
		t.Errorf("Fixtures[0].Name = %q; want %q", r.Name, "gmail list compact")
	}
	if r.ActualFormat != "toon" {
		t.Errorf("Fixtures[0].ActualFormat = %q; want toon", r.ActualFormat)
	}
	if r.ActualTokens <= 0 {
		t.Errorf("Fixtures[0].ActualTokens = %d; want > 0", r.ActualTokens)
	}
	if r.ActualTokens > 800 {
		t.Errorf("Fixtures[0].ActualTokens = %d; expected within ceiling 800", r.ActualTokens)
	}
	if !r.ActualLossy {
		t.Errorf("Fixtures[0].ActualLossy = false; want true (projection + limit)")
	}
	if res.TokenBudget.TotalFixtures != 1 || res.TokenBudget.CeilingViolations != 0 {
		t.Errorf("TokenBudget = %+v; want TotalFixtures=1, CeilingViolations=0", res.TokenBudget)
	}
	if res.TokenBudget.MaxTokensObserved != r.ActualTokens {
		t.Errorf("TokenBudget.MaxTokensObserved = %d; want %d (single fixture)",
			res.TokenBudget.MaxTokensObserved, r.ActualTokens)
	}
}

// TestRunFixturesDetectsTokenCeilingViolation verifies that a fixture whose
// shaped output exceeds expect_max_tokens fails with a ceiling-violation entry
// and increments TokenBudget.CeilingViolations.
func TestRunFixturesDetectsTokenCeilingViolation(t *testing.T) {
	src := `default_format = "toon"

[[tests]]
name = "tiny ceiling"
fixture = "gmail-list-input.json"
expect_max_tokens = 1
`
	p, err := profile.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	res, err := profile.RunFixtures(p, "testdata")
	if err != nil {
		t.Fatalf("RunFixtures: %v", err)
	}
	if res.Passed {
		t.Fatal("RunFixtures.Passed = true; expected false (1-token ceiling cannot fit any output)")
	}
	if res.TokenBudget.CeilingViolations != 1 {
		t.Errorf("TokenBudget.CeilingViolations = %d; want 1", res.TokenBudget.CeilingViolations)
	}
	if len(res.Fixtures) != 1 || res.Fixtures[0].Passed {
		t.Errorf("Fixtures[0].Passed = true or wrong count; want one failed entry")
	}
	if len(res.Fixtures[0].Failures) == 0 {
		t.Error("Fixtures[0].Failures is empty; expected a ceiling failure")
	}
}

// readFile reads the given file relative to the package directory.
func readFile(t *testing.T, path string) (string, error) {
	t.Helper()
	b, err := os.ReadFile(path)
	return string(b), err
}
