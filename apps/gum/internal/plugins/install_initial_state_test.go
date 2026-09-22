// Spec §8.7 step 2 initial state (gum-ygiq). Install used to append a state
// row carrying `installed_at: ""` and no `status`, so every downstream reader
// fell back to "active": the installed_pending_restart filter in
// internal/mcp/static_resources.go and the needs_configuration branches in
// cmd/gum/plugin_info.go had no reachable producer.
//
// These tests pin the row install writes and the boot promotion that clears
// it.

package plugins_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// installFixture copies the namespaced-plugin fixture into a temp source dir
// so a test can amend its manifest before installing.
func installFixture(t *testing.T, manifest string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	exe, err := os.ReadFile(filepath.Join(testdataDir(), "namespaced-plugin", "executable"))
	if err != nil {
		t.Fatalf("read fixture executable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "executable"), exe, 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	return src
}

const manifestNoCreds = `{
  "manifest_schema_version": 1,
  "plugin_id": "google-flights",
  "name": "Google Flights",
  "version": "0.1.0",
  "namespace_owner": "io.example.flights",
  "shape": "mcp-plugin",
  "executable": "executable",
  "advertised_tools": [
    {"name": "flights_search", "description": "Search", "risk_class": "read"}
  ],
  "declared_capabilities": {"network": true, "fs_write_dir": "", "env_allow": []}
}`

const manifestWithCreds = `{
  "manifest_schema_version": 1,
  "plugin_id": "google-flights",
  "name": "Google Flights",
  "version": "0.1.0",
  "namespace_owner": "io.example.flights",
  "shape": "mcp-plugin",
  "executable": "executable",
  "advertised_tools": [
    {"name": "flights_search", "description": "Search", "risk_class": "read"}
  ],
  "declared_capabilities": {"network": true, "fs_write_dir": "", "env_allow": []},
  "requirements": {
    "needs_user_creds": ["FLIGHTS_SESSION"],
    "credential_descriptors": [
      {"alias": "flights_session", "env": "FLIGHTS_SESSION", "kind": "session",
       "display_name": "Flights session cookie", "setup_hint": "Copy from a signed-in browser"}
    ]
  }
}`

// installOne installs manifest into a fresh profile and returns the registry
// plus the single plugin-state row.
func installOne(t *testing.T, manifest string) (*registry.Registry, map[string]any) {
	t.Helper()
	reg := registry.New(t.TempDir())
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
	if _, err := host.InstallWithRegistry(context.Background(), installFixture(t, manifest), plugins.InstallOptions{
		Registry: reg,
	}); err != nil {
		t.Fatalf("InstallWithRegistry: %v", err)
	}
	return reg, stateRow(t, reg, "google-flights")
}

func stateRow(t *testing.T, reg *registry.Registry, name string) map[string]any {
	t.Helper()
	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, raw := range files.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := row["name"].(string); got == name {
			return row
		}
	}
	t.Fatalf("plugin-state.json has no row for %q", name)
	return nil
}

func TestInstallWritesSpecInitialState(t *testing.T) {
	_, row := installOne(t, manifestNoCreds)

	installedAt, _ := row["installed_at"].(string)
	if installedAt == "" {
		t.Error("installed_at is empty; spec §8.7 step 2 requires the install timestamp")
	} else if _, err := time.Parse(time.RFC3339, installedAt); err != nil {
		t.Errorf("installed_at = %q; want RFC3339: %v", installedAt, err)
	}
	if got, _ := row["status"].(string); got != plugins.StatusInstalledPendingRestart {
		t.Errorf("status = %q; want %q", got, plugins.StatusInstalledPendingRestart)
	}
	if got, ok := row["quarantined"].(bool); !ok || got {
		t.Errorf("quarantined = %v (present=%v); want false", got, ok)
	}
	if got, ok := row["needs_configuration"].(bool); !ok || got {
		t.Errorf("needs_configuration = %v (present=%v); want false", got, ok)
	}
	if v, present := row["activated_at"]; present && v != nil {
		t.Errorf("activated_at = %v; spec §8.7 step 2 requires null at install", v)
	}
}

func TestInstallArmsNeedsConfiguration(t *testing.T) {
	_, row := installOne(t, manifestWithCreds)

	if got, _ := row["needs_configuration"].(bool); !got {
		t.Error("needs_configuration = false; a manifest with needs_user_creds must arm it")
	}
	if got, _ := row["status"].(string); got != plugins.StatusNeedsConfiguration {
		t.Errorf("status = %q; want %q", got, plugins.StatusNeedsConfiguration)
	}
}

// installFixtureWithProbe builds an install source whose executable appends
// to marker on every run, so a test can tell whether anything spawned it.
func installFixtureWithProbe(t *testing.T, manifest, marker string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	script := fmt.Sprintf("#!/bin/sh\nprintf 'spawned\\n' >> '%s'\n", marker)
	if err := os.WriteFile(filepath.Join(src, "executable"), []byte(script), 0o755); err != nil {
		t.Fatalf("write probe executable: %v", err)
	}
	return src
}

// TestInstallNeedsConfigurationSkipsLiveCanary proves the two install-time
// clauses docs/test-matrix.md row 149 states and nothing asserted: the
// needs_configuration row lands without quarantine, and install runs no live
// canary.
//
// The spy is the plugin executable. A live canary has to spawn it, so a run
// leaves a marker file behind. The control run at the end proves the marker
// works, which is what makes its absence evidence instead of a silent pass.
func TestInstallNeedsConfigurationSkipsLiveCanary(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "canary-spawned")
	src := installFixtureWithProbe(t, manifestWithCreds, marker)

	reg := registry.New(t.TempDir())
	installRoot := t.TempDir()
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	if _, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
		Registry: reg,
	}); err != nil {
		t.Fatalf("InstallWithRegistry: %v", err)
	}

	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("marker %s exists after install (stat err=%v); install must skip the live canary", marker, err)
	}

	row := stateRow(t, reg, "google-flights")
	if got, _ := row["status"].(string); got != plugins.StatusNeedsConfiguration {
		t.Errorf("status = %q; want %q", got, plugins.StatusNeedsConfiguration)
	}
	if got, ok := row["quarantined"].(bool); !ok || got {
		t.Errorf("quarantined = %v (present=%v); want false: a skipped canary is not a failed one", got, ok)
	}
	if v, present := row["activated_at"]; present && v != nil {
		t.Errorf("activated_at = %v; want null until `gum plugin setup` passes its canary", v)
	}

	// Control: the probe records a spawn when one actually happens.
	exe := filepath.Join(installRoot, "google-flights", "executable")
	if out, err := exec.Command(exe).CombinedOutput(); err != nil {
		t.Fatalf("control run of %s: %v (%s)", exe, err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("control run left no marker: %v; the probe cannot detect a canary spawn", err)
	}
}

func TestPromotePendingRestartActivatesInstalledRow(t *testing.T) {
	reg, _ := installOne(t, manifestNoCreds)

	promoted, err := plugins.PromotePendingRestart(context.Background(), reg, time.Now())
	if err != nil {
		t.Fatalf("PromotePendingRestart: %v", err)
	}
	if len(promoted) != 1 || promoted[0] != "google-flights" {
		t.Fatalf("promoted = %v; want [google-flights]", promoted)
	}
	row := stateRow(t, reg, "google-flights")
	if got, _ := row["status"].(string); got != plugins.StatusActive {
		t.Errorf("status = %q; want active", got)
	}
	if got, _ := row["activated_at"].(string); got == "" {
		t.Error("activated_at is empty after promotion")
	}
}

func TestPromotePendingRestartSkipsNeedsConfiguration(t *testing.T) {
	reg, _ := installOne(t, manifestWithCreds)

	promoted, err := plugins.PromotePendingRestart(context.Background(), reg, time.Now())
	if err != nil {
		t.Fatalf("PromotePendingRestart: %v", err)
	}
	if len(promoted) != 0 {
		t.Fatalf("promoted = %v; a needs_configuration plugin must wait for `gum plugin setup`", promoted)
	}
	row := stateRow(t, reg, "google-flights")
	if got, _ := row["status"].(string); got != plugins.StatusNeedsConfiguration {
		t.Errorf("status = %q; want needs_configuration", got)
	}
}

// PromotePendingRestart runs at every startup, so a registry with nothing to
// promote must not take the install lock or burn an install_generation.
func TestPromotePendingRestartWithoutPendingRowsSkipsWrite(t *testing.T) {
	reg, _ := installOne(t, manifestNoCreds)
	if _, err := plugins.PromotePendingRestart(context.Background(), reg, time.Now()); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	before, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, err := plugins.PromotePendingRestart(context.Background(), reg, time.Now()); err != nil {
		t.Fatalf("second promote: %v", err)
	}
	after, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if after.State.InstallGeneration != before.State.InstallGeneration {
		t.Errorf("install_generation %d → %d; a no-op promote must not write",
			before.State.InstallGeneration, after.State.InstallGeneration)
	}
}
