package main

// Tests for `gum config get|set` CLI surface.
//
// Environment isolation: every test that touches the config subsystem MUST
// redirect XDG_CONFIG_HOME (and optionally HOME) to a t.TempDir() so that
// it cannot read from or write to the developer's real ~/.config/gum tree.
//
// withTempConfigRootCLI sets XDG_CONFIG_HOME to a fresh temp directory using
// t.Setenv so the variable is automatically restored after the test.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// withTempConfigRootCLI redirects XDG_CONFIG_HOME to a t.TempDir() for the
// duration of the test. Returns the path of the temporary directory.
func withTempConfigRootCLI(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	return root
}

// runCLI executes the root cobra command with the given args, capturing stdout.
// It returns the trimmed stdout string and the first error from RunE (if any).
// The cobra command is constructed fresh for each call so tests remain isolated.
func runCLI(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()

	var buf strings.Builder
	root := newRootCmd()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)

	err = root.Execute()
	return strings.TrimSpace(buf.String()), err
}

// TestConfigSetThenGetViaCLI verifies the full set → persist → get round-trip
// using the cobra CLI surface with --profile scoping.
func TestConfigSetThenGetViaCLI(t *testing.T) {
	withTempConfigRootCLI(t)

	if _, err := runCLI(t, "--profile=default", "config", "set", "output.default_format=json"); err != nil {
		t.Fatalf("gum --profile=default config set output.default_format=json: %v", err)
	}

	out, err := runCLI(t, "--profile=default", "config", "get", "output.default_format")
	if err != nil {
		t.Fatalf("gum --profile=default config get output.default_format: %v", err)
	}
	if out != "json" {
		t.Errorf("config get output: got %q, want %q", out, "json")
	}
}

// TestConfigGetMissingKeyExitsNonZero verifies that getting a key that has
// never been set returns a non-nil error and no output.
func TestConfigGetMissingKeyExitsNonZero(t *testing.T) {
	withTempConfigRootCLI(t)

	out, err := runCLI(t, "config", "get", "nonexistent.key")
	if err == nil {
		t.Fatal("gum config get nonexistent.key: expected non-nil error, got nil")
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("gum config get nonexistent.key: expected empty stdout, got %q", out)
	}
}

// TestConfigSetRejectsMalformedArg verifies that `gum config set noequals`
// (missing the `=` separator) is rejected with a helpful error message.
func TestConfigSetRejectsMalformedArg(t *testing.T) {
	withTempConfigRootCLI(t)

	_, err := runCLI(t, "config", "set", "noequals")
	if err == nil {
		t.Fatal("gum config set noequals: expected non-nil error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "key=value") && !strings.Contains(msg, "expected key=value") && !strings.Contains(msg, "<key>=<value>") {
		t.Errorf("error message %q does not mention key=value format", msg)
	}
}

// TestConfigListEmitsKeyValuePairs verifies that after two `config set` calls,
// `config list` emits both keys=value pairs in sorted key order (Config.Keys()
// returns sorted, so list output is deterministic).
func TestConfigListEmitsKeyValuePairs(t *testing.T) {
	withTempConfigRootCLI(t)

	if _, err := runCLI(t, "config", "set", "output.default_format=json"); err != nil {
		t.Fatalf("config set output.default_format=json: %v", err)
	}
	if _, err := runCLI(t, "config", "set", "auth.subject=alice@example.com"); err != nil {
		t.Fatalf("config set auth.subject=alice@example.com: %v", err)
	}

	out, err := runCLI(t, "config", "list")
	if err != nil {
		t.Fatalf("config list: %v", err)
	}
	if !strings.Contains(out, "output.default_format=json") {
		t.Errorf("config list output missing output.default_format=json; got:\n%s", out)
	}
	if !strings.Contains(out, "auth.subject=alice@example.com") {
		t.Errorf("config list output missing auth.subject=alice@example.com; got:\n%s", out)
	}
}

// TestConfigUnsetRemovesKey verifies that `config unset <key>` removes a
// previously-set key — a subsequent `config get` returns non-zero.
func TestConfigUnsetRemovesKey(t *testing.T) {
	withTempConfigRootCLI(t)

	if _, err := runCLI(t, "config", "set", "output.default_format=json"); err != nil {
		t.Fatalf("config set: %v", err)
	}
	if _, err := runCLI(t, "config", "unset", "output.default_format"); err != nil {
		t.Fatalf("config unset: %v", err)
	}
	if _, err := runCLI(t, "config", "get", "output.default_format"); err == nil {
		t.Fatal("config get after unset: expected error, got nil")
	}
}

// TestConfigUnsetMissingKeyExitsNonZero verifies that `config unset` of a key
// that was never set returns a non-nil error.
func TestConfigUnsetMissingKeyExitsNonZero(t *testing.T) {
	withTempConfigRootCLI(t)

	if _, err := runCLI(t, "config", "unset", "nonexistent.key"); err == nil {
		t.Fatal("config unset nonexistent.key: expected non-nil error, got nil")
	}
}

// TestConfigLoadFutureSchemaSurfacesErrorCode verifies that when the config
// file on disk has a future config_schema_version, `gum config get` returns an
// error whose message contains CONFIG_SCHEMA_UNSUPPORTED.
func TestConfigLoadFutureSchemaSurfacesErrorCode(t *testing.T) {
	withTempConfigRootCLI(t)

	// Write a config file with an unsupported schema version directly.
	p, err := config.Path("default")
	if err != nil {
		t.Fatalf("config.Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("config_schema_version = 999\noutput.default_format = \"json\"\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	_, err = runCLI(t, "config", "get", "output.default_format")
	if err == nil {
		t.Fatal("gum config get with future schema version: expected non-nil error, got nil")
	}
	if !strings.Contains(err.Error(), "CONFIG_SCHEMA_UNSUPPORTED") {
		t.Errorf("error %q does not contain CONFIG_SCHEMA_UNSUPPORTED", err.Error())
	}
}
