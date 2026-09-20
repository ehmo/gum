package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProfile drops src into a temp file and returns its path.
func writeProfile(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.toml")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	return path
}

// TestProfileValidateRejectsLongOnEmpty pins ON_EMPTY_TOO_LONG at the CLI
// boundary. docs/spec.md:1584 names `gum profile validate` as the one place
// that enforces the 500-codepoint cap, because the JSON Schema deliberately
// cannot.
func TestProfileValidateRejectsLongOnEmpty(t *testing.T) {
	path := writeProfile(t, "format = \"json\"\non_empty = \""+strings.Repeat("a", 501)+"\"\n")

	_, err := runCLI(t, "profile", "validate", path)
	if err == nil {
		t.Fatal("gum profile validate exited 0; want ON_EMPTY_TOO_LONG")
	}
	if !strings.Contains(err.Error(), "ON_EMPTY_TOO_LONG") {
		t.Errorf("err = %v; want the literal code ON_EMPTY_TOO_LONG", err)
	}
}

// TestProfileTeeModeConflict is the docs/test-matrix.md proof artifact for the
// row "recovery=resource_link with tee_mode!=always fails with
// PROFILE_TEE_MODE_CONFLICT before dispatch".
func TestProfileTeeModeConflict(t *testing.T) {
	path := writeProfile(t, "format = \"json\"\nrecovery = \"resource_link\"\ntee_mode = \"failures\"\n")

	_, err := runCLI(t, "profile", "validate", path)
	if err == nil {
		t.Fatal("gum profile validate exited 0; want PROFILE_TEE_MODE_CONFLICT")
	}
	if !strings.Contains(err.Error(), "PROFILE_TEE_MODE_CONFLICT") {
		t.Errorf("err = %v; want the literal code PROFILE_TEE_MODE_CONFLICT", err)
	}
}

// TestProfileValidateAcceptsResourceLinkWithAlwaysTee keeps the valid pairing
// passing, so the new check cannot be satisfied by rejecting everything.
func TestProfileValidateAcceptsResourceLinkWithAlwaysTee(t *testing.T) {
	path := writeProfile(t, "format = \"json\"\nrecovery = \"resource_link\"\ntee_mode = \"always\"\n")

	out, err := runCLI(t, "profile", "validate", path)
	if err != nil {
		t.Fatalf("gum profile validate: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("stdout = %q; want ok", out)
	}
}

// TestProfileValidateStripNullsNeedsAVariant pins PROFILE_STRIP_NULLS_UNSAFE.
// The check is variant-bound: the safe set lives on the catalog variant, so
// --variant is what lets the validator run it. No shipped variant declares
// null_elision_safe_fields today, so any real variant rejects strip_nulls.
func TestProfileValidateStripNullsNeedsAVariant(t *testing.T) {
	path := writeProfile(t, "format = \"json\"\nstrip_nulls = true\nkeep_fields = [\"results.text\"]\n")

	_, err := runCLI(t, "profile", "validate", path,
		"--variant", "googleads.v24.rest.keywordPlanIdeas.generateKeywordIdeas")
	if err == nil {
		t.Fatal("gum profile validate --variant exited 0; want PROFILE_STRIP_NULLS_UNSAFE")
	}
	if !strings.Contains(err.Error(), "PROFILE_STRIP_NULLS_UNSAFE") {
		t.Errorf("err = %v; want the literal code PROFILE_STRIP_NULLS_UNSAFE", err)
	}
}

// TestProfileValidateSkipsStripNullsWithoutAVariant is the other half. An
// unbound file cannot supply null_elision_safe_fields, and an empty safe set
// rejects every strip_nulls profile, so the validator must skip the check and
// say it skipped rather than fail a profile it cannot judge.
func TestProfileValidateSkipsStripNullsWithoutAVariant(t *testing.T) {
	path := writeProfile(t, "format = \"json\"\nstrip_nulls = true\nkeep_fields = [\"results.text\"]\n")

	out, err := runCLI(t, "profile", "validate", path)
	if err != nil {
		t.Fatalf("gum profile validate: %v", err)
	}
	if !strings.Contains(out, "--variant") {
		t.Errorf("stdout = %q; want a note naming --variant as the way to run the strip_nulls check", out)
	}
}

// TestProfileValidateUnknownVariant keeps a typo'd binding from silently
// skipping the check it was meant to turn on.
func TestProfileValidateUnknownVariant(t *testing.T) {
	path := writeProfile(t, "format = \"json\"\nstrip_nulls = true\n")

	_, err := runCLI(t, "profile", "validate", path, "--variant", "no.such.variant")
	if err == nil {
		t.Fatal("gum profile validate --variant=no.such.variant exited 0; want VARIANT_NOT_FOUND")
	}
	if !strings.Contains(err.Error(), "VARIANT_NOT_FOUND") {
		t.Errorf("err = %v; want VARIANT_NOT_FOUND", err)
	}
}

// TestProfileTestRunsTheSemanticValidator keeps `gum profile test` on the same
// gate as `gum profile validate`; otherwise a profile rejected by one command
// still runs fixtures under the other.
func TestProfileTestRunsTheSemanticValidator(t *testing.T) {
	path := writeProfile(t, "format = \"json\"\nrecovery = \"resource_link\"\ntee_mode = \"off\"\n")

	_, err := runCLI(t, "profile", "test", path)
	if err == nil {
		t.Fatal("gum profile test exited 0; want PROFILE_TEE_MODE_CONFLICT")
	}
	if !strings.Contains(err.Error(), "PROFILE_TEE_MODE_CONFLICT") {
		t.Errorf("err = %v; want PROFILE_TEE_MODE_CONFLICT", err)
	}
}
