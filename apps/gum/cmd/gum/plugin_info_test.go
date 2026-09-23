// Spec §12 lists `info` in the `gum plugin` subcommand roster, and
// line 2520 pins its JSON root: "gum plugin info <name> --format=json |
// PluginInfo | Same object carried inside the gum://plugin/{name} JSON
// resource payload." The command was missing entirely, so an operator could
// read a plugin record over MCP but not from the CLI.
//
// These tests write the three §8.7 registry files directly, the same way
// internal/mcp/plugin_resource_test.go does, so a reader regression surfaces
// here regardless of install-path drift.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedPluginInfoFixture writes plugin-catalog.json, plugins.lock and
// plugin-state.json for one active plugin and returns the profile dir.
func seedPluginInfoFixture(t *testing.T, status string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]any{
		"plugin-catalog.json": map[string]any{
			"plugin_catalog_schema_version": 1,
			"variants": []map[string]any{
				{"variant_id": "plug.acme.b.v1", "owner_plugin": "acme"},
				{"variant_id": "plug.acme.a.v1", "owner_plugin": "acme"},
			},
		},
		"plugins.lock": map[string]any{
			"plugins_lock_schema_version": 1,
			"install_generation":          4,
			"plugins": []map[string]any{
				{
					"name":            "acme",
					"version":         "2.0.1",
					"description":     "Acme widgets.",
					"namespace_owner": "com.acme",
					"shape":           "mcp-plugin",
					"tos":             "accepted",
					"risk":            "read",
					"variant_count":   2,
					"package": map[string]any{
						"source":   "https://example.com/acme.tar.gz",
						"ref":      "v2.0.1",
						"checksum": "sha256:aaa",
					},
					"executable": map[string]any{
						"argv_normalized":   []string{"uvx", "acme"},
						"executable_sha256": "sha256:bbb",
						"install_root":      "/var/lib/gum/plugins/acme",
					},
				},
			},
		},
		"plugin-state.json": map[string]any{
			"plugin_state_schema_version": 1,
			"install_generation":          4,
			"plugins": []map[string]any{
				{
					"name":         "acme",
					"status":       status,
					"installed_at": "2026-02-01T10:00:00Z",
					"activated_at": "2026-02-01T10:01:00Z",
				},
			},
		},
	}
	for name, payload := range files {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// TestPluginInfoJSONCarriesSpecFields pins the spec §13 required
// field set on the CLI JSON root, which spec §12 declares identical to
// the gum://plugin/{name} payload.
func TestPluginInfoJSONCarriesSpecFields(t *testing.T) {
	dir := seedPluginInfoFixture(t, "active")

	out, err := formatPluginInfo(dir, "acme", "json")
	if err != nil {
		t.Fatalf("formatPluginInfo(json): %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", out, err)
	}
	for k, want := range map[string]any{
		"name":               "acme",
		"version":            "2.0.1",
		"description":        "Acme widgets.",
		"namespace_owner":    "com.acme",
		"shape":              "mcp-plugin",
		"status":             "active",
		"tos":                "accepted",
		"risk":               "read",
		"variant_count":      float64(2),
		"install_generation": float64(4),
		"activated_at":       "2026-02-01T10:01:00Z",
	} {
		if got[k] != want {
			t.Errorf("payload[%q] = %#v; want %#v", k, got[k], want)
		}
	}
	ids, _ := got["variant_ids"].([]any)
	if len(ids) != 2 || ids[0] != "plug.acme.a.v1" || ids[1] != "plug.acme.b.v1" {
		t.Errorf("variant_ids = %#v; want sorted [plug.acme.a.v1 plug.acme.b.v1]", ids)
	}
	pkg, ok := got["package"].(map[string]any)
	if !ok || pkg["checksum"] != "sha256:aaa" {
		t.Errorf("package = %#v; want checksum sha256:aaa", got["package"])
	}
	exe, ok := got["executable"].(map[string]any)
	if !ok || exe["install_root"] != "/var/lib/gum/plugins/acme" {
		t.Errorf("executable = %#v; want install_root /var/lib/gum/plugins/acme", got["executable"])
	}
}

// TestPluginInfoJSONIsCanonical asserts the JSON form is JCS-canonical, so
// the CLI bytes match the resource payload byte for byte and golden tests can
// diff them. JCS sorts object keys, so "description" precedes "name".
func TestPluginInfoJSONIsCanonical(t *testing.T) {
	dir := seedPluginInfoFixture(t, "active")

	out, err := formatPluginInfo(dir, "acme", "json")
	if err != nil {
		t.Fatalf("formatPluginInfo(json): %v", err)
	}
	if i, j := strings.Index(out, `"description"`), strings.Index(out, `"name"`); i < 0 || j < 0 || i > j {
		t.Errorf("keys not JCS-sorted in %q", out)
	}
	if strings.Contains(out, "\n  ") {
		t.Errorf("JSON is indented; JCS output is compact: %q", out)
	}
}

// TestPluginInfoTextReportsStatus pins the human form: the fields an operator
// reads first must appear, and a quarantine must name the two commands that
// clear it.
func TestPluginInfoTextReportsStatus(t *testing.T) {
	dir := seedPluginInfoFixture(t, "quarantined")

	out, err := formatPluginInfo(dir, "acme", "text")
	if err != nil {
		t.Fatalf("formatPluginInfo(text): %v", err)
	}
	for _, want := range []string{"acme", "2.0.1", "quarantined", "com.acme", "gum plugin reload", "gum plugin unquarantine"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

// TestPluginInfoUnknownPluginErrors pins the failure path. The MCP resource
// answers RESOURCE_NOT_FOUND for the same condition; the CLI carries the same
// stable code per spec §12's stderr-diagnostics rule.
func TestPluginInfoUnknownPluginErrors(t *testing.T) {
	dir := seedPluginInfoFixture(t, "active")

	_, err := formatPluginInfo(dir, "nope", "json")
	if err == nil {
		t.Fatal("formatPluginInfo(unknown) returned nil error")
	}
	if !strings.Contains(err.Error(), "RESOURCE_NOT_FOUND") {
		t.Errorf("error = %v; want RESOURCE_NOT_FOUND", err)
	}
}

// TestPluginInfoRejectsUnknownFormat keeps the --format contract closed to
// text|json, so a typo fails loudly instead of silently rendering text.
func TestPluginInfoRejectsUnknownFormat(t *testing.T) {
	dir := seedPluginInfoFixture(t, "active")

	if _, err := formatPluginInfo(dir, "acme", "toon"); err == nil {
		t.Fatal("formatPluginInfo(toon) returned nil error")
	}
}

// TestPluginInfoCmdRegistered pins the subcommand onto the `gum plugin`
// subtree: spec §12 lists it in the roster.
func TestPluginInfoCmdRegistered(t *testing.T) {
	var found bool
	for _, sub := range newPluginCmd().Commands() {
		if sub.Name() == "info" {
			found = true
			if f := sub.Flags().Lookup("format"); f == nil {
				t.Error("plugin info has no --format flag")
			}
		}
	}
	if !found {
		t.Error("`gum plugin info` is not registered under `gum plugin`")
	}
}
