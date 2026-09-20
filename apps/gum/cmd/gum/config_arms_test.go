package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	profilepkg "github.com/ehmo/gum/internal/profile"
)

// writeBrokenConfig plants a config.toml the parser rejects, under the profile
// gum will resolve from the environment.
func writeBrokenConfig(t *testing.T, configRoot, profile string) {
	t.Helper()
	dir := filepath.Join(configRoot, "gum", profile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("this line has no equals sign\n"), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
}

// TestConfigListSurfacesALoadFailure pins the load arm: an unparseable
// config.toml must fail rather than print an empty key list.
func TestConfigListSurfacesALoadFailure(t *testing.T) {
	root := withTempConfigRootCLI(t)
	writeBrokenConfig(t, root, "default")

	out, err := runCLI(t, "config", "list")
	if err == nil {
		t.Fatalf("want a config parse error, got nil; stdout=%q", out)
	}
}

// TestConfigUnsetSurfacesALoadFailure pins the same arm on the unset path.
func TestConfigUnsetSurfacesALoadFailure(t *testing.T) {
	root := withTempConfigRootCLI(t)
	writeBrokenConfig(t, root, "default")

	out, err := runCLI(t, "config", "unset", "output.default_format")
	if err == nil {
		t.Fatalf("want a config parse error, got nil; stdout=%q", out)
	}
}

// TestConfigListNotesAnEmptyProfileOnATerminal pins the gum-s985 split: the
// "no keys" note goes to stderr for a human and stdout stays empty so a pipe
// reads nothing.
func TestConfigListNotesAnEmptyProfileOnATerminal(t *testing.T) {
	withTempConfigRootCLI(t)

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() { _ = devnull.Close() }()
	if !isTerminal(devnull) {
		t.Skip("os.DevNull is not reported as a character device here")
	}

	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(devnull)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{"config", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("gum config list: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout is not empty for a profile with no keys: %q", out.String())
	}
}

// TestConfigSetExampleEchoesANonDefaultProfile pins the suggestion's profile
// echo: a pasted fix has to write the profile the caller addressed.
func TestConfigSetExampleEchoesANonDefaultProfile(t *testing.T) {
	root := newRootCmd()
	if err := root.ParseFlags([]string{"--profile", "work"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	got := configSetExample(root, []string{"output.default_format", "json"})
	if !strings.Contains(got, "--profile work") {
		t.Errorf("suggestion %q drops the caller's profile", got)
	}
}

// TestResolveProfileFlagFallsBackOnABadName pins the flag helper's fallback.
// Callers that cannot fail use it, so an unparseable name has to read as the
// default profile rather than an empty string.
func TestResolveProfileFlagFallsBackOnABadName(t *testing.T) {
	root := newRootCmd()
	if err := root.ParseFlags([]string{"--profile", "bad/name"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if got := resolveProfileFlag(root); got != profilepkg.DefaultName.String() {
		t.Errorf("resolveProfileFlag = %q; want %q", got, profilepkg.DefaultName.String())
	}
}
