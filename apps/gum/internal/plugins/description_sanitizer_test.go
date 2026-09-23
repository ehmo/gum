// Tests for the spec §5.4 description sanitizer on the plugin manifest path
// (gum-ya9h). Before this gate, advertised_tools[].description reached
// gum.describe_op and the MCP tool list with no sanitizer pass at all, so a
// manifest could serve marketing copy, a second-person address, PII, or an
// undisclosed destructive tool straight into the model's context.
//
// LoadManifest is the checkpoint rather than install, because install runs
// once while LoadManifest runs on install, list, and every spawn. A manifest
// edited in place after install is therefore rejected at the next spawn.
package plugins_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// writeToolManifest writes a manifest whose single advertised tool carries the
// given description and risk class, and returns the install dir.
func writeToolManifest(t *testing.T, description, riskClass string) string {
	t.Helper()
	dir := t.TempDir()

	exe := filepath.Join(dir, "plugin-exe")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writeToolManifest: write executable: %v", err)
	}

	manifest := `{
  "manifest_schema_version": 1,
  "plugin_id": "test-desc-plugin",
  "name": "Test Description Plugin",
  "version": "0.1.0",
  "shape": "mcp-plugin",
  "executable": "plugin-exe",
  "advertised_tools": [
    {"name": "do_thing", "description": ` + quoteJSON(description) + `, "risk_class": "` + riskClass + `"}
  ],
  "declared_capabilities": {
    "network": false,
    "fs_write_dir": "",
    "env_allow": []
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("writeToolManifest: write manifest.json: %v", err)
	}
	return dir
}

// quoteJSON renders s as a JSON string literal. The descriptions under test
// carry no control characters, so escaping quotes and backslashes suffices.
func quoteJSON(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// TestLoadManifestAcceptsCleanDescription pins the pass case, so the gate
// cannot be satisfied by rejecting everything.
func TestLoadManifestAcceptsCleanDescription(t *testing.T) {
	dir := writeToolManifest(t, "Search patents by keyword and assignee.", "read")

	m, err := plugins.LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest = %v; want nil for a clean read-class description", err)
	}
	if m == nil {
		t.Fatal("LoadManifest returned nil manifest without error")
	}
}

func TestLoadManifestRejectsDescriptionRules(t *testing.T) {
	cases := []struct {
		name        string
		description string
		riskClass   string
		wantRule    string
	}{
		{
			name:        "marketing",
			description: "A revolutionary, best-in-class patent search.",
			riskClass:   "read",
			wantRule:    "rule 1",
		},
		{
			name:        "model hint",
			description: "Patent search designed for AI agents.",
			riskClass:   "read",
			wantRule:    "rule 2",
		},
		{
			name:        "second person",
			description: "Finds the patents you care about.",
			riskClass:   "read",
			wantRule:    "rule 3",
		},
		{
			name:        "undisclosed destructive",
			description: "Moves a record to the archive area.",
			riskClass:   "destructive",
			wantRule:    "rule 6",
		},
		{
			name:        "pii",
			description: "Routes results to support@example.com.",
			riskClass:   "read",
			wantRule:    "rule 7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeToolManifest(t, tc.description, tc.riskClass)

			_, err := plugins.LoadManifest(dir)
			if err == nil {
				t.Fatalf("LoadManifest = nil; want ErrManifestInvalid for %q", tc.description)
			}
			if !errors.Is(err, plugins.ErrManifestInvalid) {
				t.Fatalf("LoadManifest = %v; want errors.Is(err, ErrManifestInvalid)", err)
			}
			if !strings.Contains(err.Error(), tc.wantRule) {
				t.Errorf("LoadManifest = %v; want the message to name %s", err, tc.wantRule)
			}
			if !strings.Contains(err.Error(), "do_thing") {
				t.Errorf("LoadManifest = %v; want the message to name the offending tool", err)
			}
		})
	}
}

// TestLoadManifestAcceptsDisclosedDestructive pins the rule-6 pass case: a
// destructive tool that names the loss loads.
func TestLoadManifestAcceptsDisclosedDestructive(t *testing.T) {
	dir := writeToolManifest(t, "Permanently deletes a stored record. Cannot be undone.", "destructive")

	if _, err := plugins.LoadManifest(dir); err != nil {
		t.Fatalf("LoadManifest = %v; want nil for a disclosed destructive description", err)
	}
}
