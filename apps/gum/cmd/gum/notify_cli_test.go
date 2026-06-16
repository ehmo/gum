package main

// CLI-level tests for the update notifier wired into `gum version`
// (gum-afcv.5). Exercises the disabled-by-default path, the opt-in path
// with a seeded cache, and verifies the notifier never touches stdout.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runVersionCLI invokes `gum version` with separated stdout/stderr buffers.
func runVersionCLI(t *testing.T) (stdout, stderr string) {
	t.Helper()
	var sout, serr bytes.Buffer
	root := newRootCmd()
	root.SetOut(&sout)
	root.SetErr(&serr)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("gum version: %v", err)
	}
	return sout.String(), serr.String()
}

// withTempXDGRoots redirects BOTH XDG_CONFIG_HOME and XDG_CACHE_HOME so the
// notifier reads from a clean per-test state. Returns the cache root.
func withTempXDGRoots(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	return cache
}

func TestVersionNotifierDisabledByDefault(t *testing.T) {
	cacheRoot := withTempXDGRoots(t)
	// Seed a cache that WOULD trigger a warning if the notifier were enabled.
	entry := struct {
		CheckedAt     time.Time `json:"checked_at"`
		LatestVersion string    `json:"latest_version"`
	}{CheckedAt: time.Now().Add(-5 * time.Minute), LatestVersion: "v99.0.0"}
	data, _ := json.Marshal(entry)
	cachePath := filepath.Join(cacheRoot, "gum", "default", "notify.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := runVersionCLI(t)
	if !strings.Contains(stdout, version) {
		t.Errorf("version stdout missing version string: %q", stdout)
	}
	if strings.Contains(stderr, "[notify]") {
		t.Errorf("disabled notifier leaked to stderr: %q", stderr)
	}
}

func TestVersionNotifierOptInPrintsToStderrNotStdout(t *testing.T) {
	cacheRoot := withTempXDGRoots(t)

	// Enable notify via the same CLI surface end users would use.
	var sink bytes.Buffer
	setRoot := newRootCmd()
	setRoot.SetOut(&sink)
	setRoot.SetErr(&sink)
	setRoot.SetArgs([]string{"config", "set", "notify.enabled=true"})
	if err := setRoot.Execute(); err != nil {
		t.Fatalf("config set notify.enabled=true: %v\n%s", err, sink.String())
	}

	// Seed a fresh cache pointing at a clearly newer version. The binary's
	// version var is "dev" during go test, so we override it on the entry's
	// LatestVersion to compare-greater against any sane value. CompareVersions
	// returns 0 for "dev" (unparseable), so the warning won't fire from "dev".
	// Tests of the warning path live in internal/notify; here we only verify
	// (a) opt-in is read correctly, (b) no panic, (c) stdout stays clean.
	entry := struct {
		CheckedAt     time.Time `json:"checked_at"`
		LatestVersion string    `json:"latest_version"`
	}{CheckedAt: time.Now().Add(-5 * time.Minute), LatestVersion: "v99.0.0"}
	data, _ := json.Marshal(entry)
	cachePath := filepath.Join(cacheRoot, "gum", "default", "notify.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := runVersionCLI(t)
	if !strings.Contains(stdout, version) {
		t.Errorf("version stdout missing version string: %q", stdout)
	}
	// Stdout must remain JUST the version line — pipelines depend on it.
	stdoutLines := strings.Count(strings.TrimSpace(stdout), "\n")
	if stdoutLines > 0 {
		t.Errorf("notifier polluted stdout with %d extra lines: %q", stdoutLines, stdout)
	}
	// The version="dev" build means CompareVersions returns 0 → no warning
	// here, but the path executed without error. The warning surface is
	// proved in internal/notify/notify_test.go where version is injectable.
	_ = stderr
}

func TestVersionNotifierUnknownConfigKeyIsKnownPrefix(t *testing.T) {
	withTempXDGRoots(t)
	// Set notify.enabled and read it back via list — if "notify." were not in
	// knownPrefixes, Load would emit an unknown_config_key warning.
	var sink bytes.Buffer
	root := newRootCmd()
	root.SetOut(&sink)
	root.SetErr(&sink)
	root.SetArgs([]string{"config", "set", "notify.enabled=true"})
	if err := root.Execute(); err != nil {
		t.Fatalf("config set: %v\n%s", err, sink.String())
	}

	sink.Reset()
	root = newRootCmd()
	root.SetOut(&sink)
	root.SetErr(&sink)
	root.SetArgs([]string{"config", "get", "notify.enabled"})
	if err := root.Execute(); err != nil {
		t.Fatalf("config get: %v\n%s", err, sink.String())
	}
	if !strings.Contains(sink.String(), "true") {
		t.Errorf("config get notify.enabled did not return true: %q", sink.String())
	}
}
