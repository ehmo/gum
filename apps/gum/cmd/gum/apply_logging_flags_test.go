package main

import (
	"testing"
)

// TestApplyLoggingFlagsValidJSONDefault drives the happy path: no env,
// no flag overrides → info-level JSON handler installed without error.
func TestApplyLoggingFlagsValidJSONDefault(t *testing.T) {
	root := newRootCmd()
	if err := applyLoggingFlags(root); err != nil {
		t.Errorf("applyLoggingFlags: %v", err)
	}
}

// TestApplyLoggingFlagsTextFormat exercises the alternative format
// branch. ParseFlags is required to merge PersistentFlags into the
// local FlagSet that applyLoggingFlags reads.
func TestApplyLoggingFlagsTextFormat(t *testing.T) {
	root := newRootCmd()
	if err := root.ParseFlags([]string{"--log-format=text"}); err != nil {
		t.Fatal(err)
	}
	if err := applyLoggingFlags(root); err != nil {
		t.Errorf("text format: %v", err)
	}
}

// TestApplyLoggingFlagsInvalidLevelFlag drives the explicit-flag error
// branch: a bogus --log-level must return the operator-facing error
// listing the closed enum.
func TestApplyLoggingFlagsInvalidLevelFlag(t *testing.T) {
	root := newRootCmd()
	if err := root.ParseFlags([]string{"--log-level=noisy"}); err != nil {
		t.Fatal(err)
	}
	err := applyLoggingFlags(root)
	if err == nil {
		t.Fatal("expected error for invalid --log-level")
	}
}

// TestApplyLoggingFlagsInvalidFormat drives the format-error branch.
func TestApplyLoggingFlagsInvalidFormat(t *testing.T) {
	root := newRootCmd()
	if err := root.ParseFlags([]string{"--log-format=xml"}); err != nil {
		t.Fatal(err)
	}
	err := applyLoggingFlags(root)
	if err == nil {
		t.Fatal("expected error for invalid --log-format")
	}
}

// TestApplyLoggingFlagsValidLevelFlag pins the
// `parseLogLevel(flag) ok → level = l` arm (root.go:127-129). A valid
// explicit --log-level on the CLI MUST override the env/default; this
// test pins that arm distinct from the invalid-flag arm covered above.
func TestApplyLoggingFlagsValidLevelFlag(t *testing.T) {
	root := newRootCmd()
	if err := root.ParseFlags([]string{"--log-level=debug"}); err != nil {
		t.Fatal(err)
	}
	if err := applyLoggingFlags(root); err != nil {
		t.Errorf("valid --log-level=debug: %v", err)
	}
}

// TestApplyLoggingFlagsEnvFallback covers the GUM_LOG_LEVEL env path
// (CLI flag unchanged → env wins).
func TestApplyLoggingFlagsEnvFallback(t *testing.T) {
	t.Setenv("GUM_LOG_LEVEL", "debug")
	root := newRootCmd()
	if err := applyLoggingFlags(root); err != nil {
		t.Errorf("env fallback: %v", err)
	}
}

// TestApplyLoggingFlagsEnvIgnoredWhenInvalid documents the silent
// drop: a bogus env value falls through to the info default (no error)
// because env is best-effort and shouldn't fail an unrelated command.
func TestApplyLoggingFlagsEnvIgnoredWhenInvalid(t *testing.T) {
	t.Setenv("GUM_LOG_LEVEL", "trace")
	root := newRootCmd()
	if err := applyLoggingFlags(root); err != nil {
		t.Errorf("invalid env should be silently ignored, got: %v", err)
	}
}

// TestLoadCatalogHappyPath covers the catalog.json unmarshal path:
// when embedded.CatalogJSON is non-empty (it always is in normal builds)
// the function returns a non-nil snapshot.
func TestLoadCatalogHappyPath(t *testing.T) {
	c := loadCatalog()
	if c == nil {
		t.Fatal("loadCatalog returned nil; expected non-nil snapshot from embedded JSON")
	}
}
