package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileValidateOK parses a known-good fixture and expects "ok".
func TestProfileValidateOK(t *testing.T) {
	root := findRepoRoot(t)
	cmd := newRootCmd()
	cmd.SetArgs([]string{"profile", "validate",
		filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-profile.toml"),
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !strings.Contains(stdout.String(), "ok") {
		t.Errorf("expected 'ok' in output, got %q", stdout.String())
	}
}

// TestProfileValidateRejectsBadFormat ensures malformed profiles fail.
func TestProfileValidateRejectsBadFormat(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(bad, []byte(`default_format = "bogus"`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	cmd.SetArgs([]string{"profile", "validate", bad})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error for bad profile, got none. stdout=%q", stdout.String())
	}
}

// TestProfileTestMatchesGolden runs `profile test` against the gmail golden.
func TestProfileTestMatchesGolden(t *testing.T) {
	root := findRepoRoot(t)
	cmd := newRootCmd()
	cmd.SetArgs([]string{"profile", "test",
		filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-profile.toml"),
		"--input", filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-input.json"),
		"--golden", filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-golden.toon"),
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("profile test: %v\nstdout=%s", err, stdout.String())
	}
}

// TestProfileTestRequiresInputOrTests rejects calls that have neither --input
// nor any [[tests]] entries in the profile (spec §12.1: release-gated profiles
// MUST include at least one fixture).
func TestProfileTestRequiresInputOrTests(t *testing.T) {
	root := findRepoRoot(t)
	cmd := newRootCmd()
	cmd.SetArgs([]string{"profile", "test",
		filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-profile.toml"),
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error without --input and without [[tests]], got nil; stdout=%q", stdout.String())
	}
	if !strings.Contains(err.Error(), "PROFILE_NO_FIXTURES") {
		t.Errorf("expected PROFILE_NO_FIXTURES error, got: %v", err)
	}
}

// TestProfileTestEmitsFixtureRunResultJSON verifies the spec §12.1 contract:
// `gum profile test --format=json` (without --input) emits the
// `{passed, fixtures: ProfileFixtureResult[], token_budget: TokenBudgetSummary}`
// envelope when [[tests]] entries are present in the profile.
func TestProfileTestEmitsFixtureRunResultJSON(t *testing.T) {
	root := findRepoRoot(t)
	cmd := newRootCmd()
	cmd.SetArgs([]string{"profile", "test",
		filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-with-tests.toml"),
		"--format", "json",
	})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("profile test --format=json: %v\nstdout=%s", err, stdout.String())
	}
	raw := stdout.String()

	// Validate envelope shape: top-level passed, fixtures array, token_budget object.
	var env struct {
		Passed      bool `json:"passed"`
		Fixtures    []struct {
			Name         string `json:"name"`
			Passed       bool   `json:"passed"`
			ActualFormat string `json:"actual_format"`
			ActualTokens int    `json:"actual_tokens"`
		} `json:"fixtures"`
		TokenBudget struct {
			TotalFixtures     int `json:"total_fixtures"`
			TotalTokens       int `json:"total_tokens"`
			MaxTokensObserved int `json:"max_tokens_observed"`
			CeilingViolations int `json:"ceiling_violations"`
		} `json:"token_budget"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode JSON envelope: %v\nraw=%s", err, raw)
	}
	if !env.Passed {
		t.Errorf("envelope.passed = false; expected true; raw=%s", raw)
	}
	if len(env.Fixtures) != 1 {
		t.Fatalf("len(fixtures) = %d; want 1; raw=%s", len(env.Fixtures), raw)
	}
	if env.Fixtures[0].Name != "gmail list compact" {
		t.Errorf("fixtures[0].name = %q; want %q", env.Fixtures[0].Name, "gmail list compact")
	}
	if env.Fixtures[0].ActualFormat != "toon" {
		t.Errorf("fixtures[0].actual_format = %q; want toon", env.Fixtures[0].ActualFormat)
	}
	if env.Fixtures[0].ActualTokens <= 0 {
		t.Errorf("fixtures[0].actual_tokens = %d; want > 0", env.Fixtures[0].ActualTokens)
	}
	if env.TokenBudget.TotalFixtures != 1 {
		t.Errorf("token_budget.total_fixtures = %d; want 1", env.TokenBudget.TotalFixtures)
	}
	if env.TokenBudget.CeilingViolations != 0 {
		t.Errorf("token_budget.ceiling_violations = %d; want 0", env.TokenBudget.CeilingViolations)
	}
}

// TestProfileTestExitsNonZeroOnFixtureFailure verifies that fixture failures
// surface as a non-zero exit (cobra returns an error) per spec §12.1.
func TestProfileTestExitsNonZeroOnFixtureFailure(t *testing.T) {
	dir := t.TempDir()
	root := findRepoRoot(t)
	// Profile with a 1-token ceiling that cannot accommodate any real output.
	profilePath := filepath.Join(dir, "failing.toml")
	flatInput := filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-flat-input.json")
	src := `default_format = "toon"
[[tests]]
name = "tiny"
fixture = "` + flatInput + `"
expect_max_tokens = 1
`
	if err := os.WriteFile(profilePath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newRootCmd()
	cmd.SetArgs([]string{"profile", "test", profilePath, "--format", "json"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error for failed fixture, got nil; stdout=%s", stdout.String())
	}
}

// TestProfileTestErrorBranches pins the remaining error paths in
// newProfileTestCmd so a refactor that silently swallows them is caught.
func TestProfileTestErrorBranches(t *testing.T) {
	root := findRepoRoot(t)
	goodProfile := filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-profile.toml")
	goodInput := filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-input.json")

	dir := t.TempDir()
	badSyntax := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(badSyntax, []byte(`default_format = "bogus"`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("profile_path_not_found", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"profile", "test", filepath.Join(dir, "missing.toml"), "--input", goodInput})
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "read profile") {
			t.Errorf("want 'read profile' error; got %v", err)
		}
	})

	t.Run("invalid_profile_syntax", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"profile", "test", badSyntax, "--input", goodInput})
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "invalid profile") {
			t.Errorf("want 'invalid profile' error; got %v", err)
		}
	})

	t.Run("unsupported_format_in_fixture_runner", func(t *testing.T) {
		// Profile with at least one [[tests]] entry so we reach the format switch.
		withTests := filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-with-tests.toml")
		cmd := newRootCmd()
		cmd.SetArgs([]string{"profile", "test", withTests, "--format", "raw"})
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "not supported in fixture-runner mode") {
			t.Errorf("want fixture-runner unsupported-format error; got %v", err)
		}
	})

	t.Run("input_path_not_found", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"profile", "test", goodProfile, "--input", filepath.Join(dir, "missing.json")})
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "read input") {
			t.Errorf("want 'read input' error; got %v", err)
		}
	})

	t.Run("golden_path_not_found", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"profile", "test", goodProfile, "--input", goodInput, "--golden", filepath.Join(dir, "no-golden")})
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "read golden") {
			t.Errorf("want 'read golden' error; got %v", err)
		}
	})

	t.Run("golden_mismatch_emits_diff_error", func(t *testing.T) {
		wrongGolden := filepath.Join(dir, "wrong.golden")
		if err := os.WriteFile(wrongGolden, []byte("definitely-not-the-real-output"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := newRootCmd()
		cmd.SetArgs([]string{"profile", "test", goodProfile, "--input", goodInput, "--golden", wrongGolden})
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "PROFILE_GOLDEN_MISMATCH") {
			t.Errorf("want PROFILE_GOLDEN_MISMATCH error; got %v", err)
		}
	})

	t.Run("single_fixture_no_golden_writes_to_stdout_with_newline", func(t *testing.T) {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"profile", "test", goodProfile, "--input", goodInput})
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetErr(&buf)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute: %v\nstdout=%s", err, buf.String())
		}
		out := buf.String()
		if out == "" {
			t.Errorf("expected non-empty stdout")
		}
		if out[len(out)-1] != '\n' {
			t.Errorf("expected trailing newline; got %q", out[len(out)-1])
		}
	})
}

// findRepoRoot walks up from cwd to find the apps/gum directory (works in tests
// regardless of where `go test` is invoked).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("could not find apps/gum root from %s", wd)
	return ""
}
