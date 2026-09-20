package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFindCatalogVariantWithoutACatalog pins the nil guard. A binary built
// without an embedded catalog must return "no such variant", not panic.
func TestFindCatalogVariantWithoutACatalog(t *testing.T) {
	if v := findCatalogVariant(nil, "gmail.messages.list"); v != nil {
		t.Errorf("findCatalogVariant(nil, ...) = %+v; want nil", v)
	}
}

// TestCatalogTargetIDsWithoutACatalog pins the same guard on the binding-key
// side: no catalog means no known ids, which is what turns the key check off.
func TestCatalogTargetIDsWithoutACatalog(t *testing.T) {
	if ids := catalogTargetIDs(nil); ids != nil {
		t.Errorf("catalogTargetIDs(nil) = %v; want nil", ids)
	}
}

// TestProfileTestSurfacesAnEncodeFailure pins the fixture-runner write arm: a
// closed stdout must fail the command instead of reporting a silent pass.
func TestProfileTestSurfacesAnEncodeFailure(t *testing.T) {
	root := findRepoRoot(t)
	withTests := filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-with-tests.toml")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"profile", "test", withTests, "--format", "json"})
	cmd.SetOut(failWriter{})
	cmd.SetErr(failWriter{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "encode json") {
		t.Errorf("want an 'encode json' error; got %v", err)
	}
}

// TestProfileTestSurfacesAnApplyFailure pins the single-fixture apply arm. An
// input that is not JSON cannot be shaped, and the command must say so.
func TestProfileTestSurfacesAnApplyFailure(t *testing.T) {
	root := findRepoRoot(t)
	goodProfile := filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-profile.toml")

	badInput := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(badInput, []byte("not json at all"), 0o600); err != nil {
		t.Fatalf("write %s: %v", badInput, err)
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"profile", "test", goodProfile, "--input", badInput})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "apply profile") {
		t.Errorf("want an 'apply profile' error; got %v", err)
	}
}

// TestProfileTestSurfacesABodyWriteFailure pins the single-fixture write arm:
// the shaped body failing to reach stdout has to fail the command.
func TestProfileTestSurfacesABodyWriteFailure(t *testing.T) {
	root := findRepoRoot(t)
	goodProfile := filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-profile.toml")
	goodInput := filepath.Join(root, "internal", "output", "profile", "testdata", "gmail-list-input.json")

	cmd := newRootCmd()
	cmd.SetArgs([]string{"profile", "test", goodProfile, "--input", goodInput})
	cmd.SetOut(failWriter{})
	cmd.SetErr(failWriter{})
	if err := cmd.Execute(); err == nil {
		t.Error("want the stdout write failure, got nil")
	}
}
