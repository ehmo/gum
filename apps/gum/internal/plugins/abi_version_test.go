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
	"encoding/json"
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
// (pre-normative, always unsupported) returns ErrUnsupportedSchemaVersion.
//
// Spec §8.6: "A missing or future version fails with the same code; no
// manifest is exempt." An explicit 0 is always unsupported.
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

// TestPluginManifestSchemaVersionPlacement pins docs/test-matrix.md row 79
// and spec §8.6: the canonical version field is a top-level
// sibling of `plugin`. A missing field and a copy nested inside `plugin`
// both fail install with PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED, and the nested
// copy fails even when it names the supported version.
func TestPluginManifestSchemaVersionPlacement(t *testing.T) {
	cases := []struct {
		name     string
		manifest map[string]any
		wantErr  error
	}{
		{
			name: "top-level version accepted",
			manifest: map[string]any{
				"manifest_schema_version": 1,
			},
		},
		{
			name:     "missing version rejected",
			manifest: map[string]any{},
			wantErr:  plugins.ErrUnsupportedSchemaVersion,
		},
		{
			name: "nested version rejected",
			manifest: map[string]any{
				"plugin": map[string]any{"manifest_schema_version": 1},
			},
			wantErr: plugins.ErrUnsupportedSchemaVersion,
		},
		{
			name: "nested version rejected even beside a valid top-level one",
			manifest: map[string]any{
				"manifest_schema_version": 1,
				"plugin":                  map[string]any{"manifest_schema_version": 1},
			},
			wantErr: plugins.ErrUnsupportedSchemaVersion,
		},
		{
			name: "unrelated plugin table accepted",
			manifest: map[string]any{
				"manifest_schema_version": 1,
				"plugin":                  map[string]any{"description": "a plugin"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writePlacementManifest(t, tc.manifest)
			_, err := plugins.LoadManifest(dir)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("LoadManifest = %v; want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("LoadManifest = %v; want PLUGIN_MANIFEST_SCHEMA_UNSUPPORTED", err)
			}
		})
	}
}

// writePlacementManifest writes a minimal valid manifest merged with extra,
// so each case controls only the version placement under test.
func writePlacementManifest(t *testing.T, extra map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	man := map[string]any{
		"plugin_id":  "acme",
		"name":       "acme",
		"version":    "1.0.0",
		"shape":      "mcp-plugin",
		"executable": "bin/acme",
		"advertised_tools": []map[string]any{{
			"name":       "ping",
			"risk_class": "read",
		}},
	}
	for k, v := range extra {
		man[k] = v
	}
	b, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
