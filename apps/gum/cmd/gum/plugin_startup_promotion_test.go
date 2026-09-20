package main

// Spec §8.7 startup activation (gum-ygiq). `gum plugin install` parks a new
// plugin in installed_pending_restart; the next process boot is what flips it
// to active. Before this, PromotePendingRestart had no production caller, so
// a pending row stayed pending forever.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writePluginState writes plugin-state.json under the data root for profile.
func writePluginState(t *testing.T, dataRoot, profile string, rows []map[string]any) string {
	t.Helper()
	p := filepath.Join(dataRoot, "gum", profile, "plugin-state.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"plugin_state_schema_version": 1,
		"plugins":                     rows,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func readPluginStateRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plugin-state.json: %v", err)
	}
	var parsed struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal plugin-state.json: %v", err)
	}
	return parsed.Plugins
}

func TestCLIStartupPromotesPendingPlugin(t *testing.T) {
	withTempConfigRootCLI(t)
	root := withTempDataRootCLI(t)
	statePath := writePluginState(t, root, "default", []map[string]any{
		{"name": "acme-plug", "status": "installed_pending_restart", "quarantined": false},
	})

	if _, err := runCLI(t, "--profile=default", "version"); err != nil {
		t.Fatalf("gum version: %v", err)
	}

	rows := readPluginStateRows(t, statePath)
	if len(rows) != 1 {
		t.Fatalf("plugin-state rows = %d; want 1", len(rows))
	}
	if got, _ := rows[0]["status"].(string); got != "active" {
		t.Errorf("status = %q; want active after startup promotion", got)
	}
	if got, _ := rows[0]["activated_at"].(string); got == "" {
		t.Error("activated_at is empty after startup promotion")
	}
}

func TestCLIStartupLeavesQuarantinedPluginPending(t *testing.T) {
	withTempConfigRootCLI(t)
	root := withTempDataRootCLI(t)
	statePath := writePluginState(t, root, "default", []map[string]any{
		{"name": "acme-plug", "status": "installed_pending_restart", "quarantined": true},
	})

	if _, err := runCLI(t, "--profile=default", "version"); err != nil {
		t.Fatalf("gum version: %v", err)
	}

	rows := readPluginStateRows(t, statePath)
	if got, _ := rows[0]["status"].(string); got != "installed_pending_restart" {
		t.Errorf("status = %q; a quarantined plugin must stay pending", got)
	}
}

// A profile with no plugin registry must not gain one just because a command
// ran.
func TestCLIStartupDoesNotCreateRegistryFiles(t *testing.T) {
	withTempConfigRootCLI(t)
	root := withTempDataRootCLI(t)

	if _, err := runCLI(t, "--profile=default", "version"); err != nil {
		t.Fatalf("gum version: %v", err)
	}

	for _, name := range []string{"plugin-state.json", "plugins.lock", "plugin-catalog.json", "plugins.install.lock"} {
		p := filepath.Join(root, "gum", "default", name)
		if _, err := os.Stat(p); err == nil {
			t.Errorf("startup created %s; it must be a pure read when nothing is pending", p)
		}
	}
}
