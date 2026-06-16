// Package plugins_test — RED tests for plugin ABI version rejection (gum-5wwz).
//
// Covers the manifest_schema_version gate defined in docs/catalog-abi.md
// § "Versioned Artifacts" and spec.md §8.6:
//
//   - PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED  for manifest.json manifest_schema_version mismatches
//
// The existing TestLoadManifestUnsupportedSchemaVersion covers version=999 →
// ErrUnsupportedSchemaVersion. This file extends coverage with explicit
// too-old (version=0) and too-new (version=9999) sub-cases and the accepted
// (version=1) pass case — matching the canonical three-way pattern for all ABI gates.
package plugins_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestPluginManifestVersionRejectedTooNew verifies that manifest_schema_version=9999
// (unsupported future) returns ErrUnsupportedSchemaVersion (== PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED).
func TestPluginManifestVersionRejectedTooNew(t *testing.T) {
	dir := writeManifestDir(t, 9999)

	_, err := plugins.LoadManifest(dir)
	if err == nil {
		t.Fatal("LoadManifest returned nil error; want ErrUnsupportedSchemaVersion for version=9999")
	}
	if !errors.Is(err, plugins.ErrUnsupportedSchemaVersion) {
		t.Fatalf("LoadManifest = %v; want errors.Is(err, ErrUnsupportedSchemaVersion)", err)
	}
}

// TestPluginManifestVersionRejectedTooOld verifies that manifest_schema_version=0
// (pre-normative, treated as unsupported for third-party manifests) returns
// ErrUnsupportedSchemaVersion.
//
// Spec §8.6: "missing is treated as 1 only for bundled v0.1.0 development manifests,
// not third-party installs." An explicit 0 is always unsupported.
func TestPluginManifestVersionRejectedTooOld(t *testing.T) {
	dir := writeManifestDir(t, 0)

	_, err := plugins.LoadManifest(dir)
	if err == nil {
		t.Fatal("LoadManifest returned nil error; want ErrUnsupportedSchemaVersion for version=0")
	}
	if !errors.Is(err, plugins.ErrUnsupportedSchemaVersion) {
		t.Fatalf("LoadManifest = %v; want errors.Is(err, ErrUnsupportedSchemaVersion)", err)
	}
}

// TestPluginManifestVersionAccepted verifies that manifest_schema_version=1 is accepted.
func TestPluginManifestVersionAccepted(t *testing.T) {
	dir := writeManifestDir(t, 1)

	m, err := plugins.LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest returned %v; want nil for manifest_schema_version=1", err)
	}
	if m == nil {
		t.Fatal("LoadManifest returned nil manifest without error")
	}
}

// ── helper ───────────────────────────────────────────────────────────────────

// writeManifestDir creates a temp directory with a manifest.json bearing the
// given manifest_schema_version and a stub executable.
func writeManifestDir(t *testing.T, version int) string {
	t.Helper()
	dir := t.TempDir()

	// Write a stub executable so the manifest's "executable" field resolves.
	exe := filepath.Join(dir, "plugin-exe")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writeManifestDir: write executable: %v", err)
	}

	manifest := buildManifestJSON(version)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("writeManifestDir: write manifest.json: %v", err)
	}
	return dir
}

// buildManifestJSON returns a minimal valid manifest JSON with the given version.
func buildManifestJSON(version int) string {
	return `{
  "manifest_schema_version": ` + itoa(version) + `,
  "plugin_id": "test-abi-plugin",
  "name": "Test ABI Plugin",
  "version": "0.1.0",
  "shape": "mcp-plugin",
  "executable": "plugin-exe",
  "advertised_tools": [
    {"name": "do_thing", "description": "Does a thing.", "risk_class": "read"}
  ],
  "declared_capabilities": {
    "network": false,
    "fs_write_dir": "",
    "env_allow": []
  }
}`
}

// itoa converts an int to its string representation without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
