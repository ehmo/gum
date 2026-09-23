package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// pluginNamespacePrefix is the literal every plugin op_id carries. The
// separation between plugin and first-party op_ids rests on it: pluginOpID
// hardcodes it, and no first-party catalog op may claim it.
const pluginNamespacePrefix = "plug."

// firstPartyPrefixesUnderThreat are the Google service prefixes a hostile
// plugin would most want. They are the set docs/spec.md §5.1 once proposed
// to protect with a static reserved list (bead gum-g9qv).
var firstPartyPrefixesUnderThreat = []string{
	"gmail", "drive", "calendar", "docs", "sheets", "slides", "meet",
	"chat", "forms", "tasks", "keep", "bigquery", "compute", "gke",
	"cloudrun", "storage", "spanner", "pubsub", "logging", "monitoring",
	"maps", "gemini", "admin", "classroom",
}

// TestPluginOpIDStaysUnderPlugNamespace pins the structural guarantee that
// replaces the reserved-prefix list: a manifest cannot reach a first-party
// op_id no matter what plugin_id it declares, because pluginOpID prepends
// "plug." unconditionally. A plugin calling itself "gmail" produces
// plug.gmail.search, never gmail.search.
func TestPluginOpIDStaysUnderPlugNamespace(t *testing.T) {
	for _, prefix := range firstPartyPrefixesUnderThreat {
		opID := pluginOpID(prefix, "search")

		if !strings.HasPrefix(opID, pluginNamespacePrefix) {
			t.Errorf("pluginOpID(%q, \"search\") = %q; want the %q prefix", prefix, opID, pluginNamespacePrefix)
		}
		if strings.HasPrefix(opID, prefix+".") || opID == prefix {
			t.Errorf("pluginOpID(%q, \"search\") = %q; a plugin reached the first-party %q namespace", prefix, opID, prefix)
		}

		variantID := pluginVariantID(prefix, "search")
		if !strings.HasPrefix(variantID, pluginNamespacePrefix) {
			t.Errorf("pluginVariantID(%q, \"search\") = %q; want the %q prefix", prefix, variantID, pluginNamespacePrefix)
		}
	}
}

// TestPluginIDRejectsDottedValue pins the other half of the guarantee. A
// dotted plugin_id such as "gmail.search" would make pluginOpID emit
// plug.gmail.search.<tool>, and a leading-dot value would let the manifest
// steer the segment after "plug.". pluginIDRe rejects both, so the op_id
// always has exactly three dot-separated parts.
func TestPluginIDRejectsDottedValue(t *testing.T) {
	rejected := []string{
		"gmail.search",   // dotted: would add a segment
		".gmail",         // leading dot
		"gmail.",         // trailing dot
		"Gmail",          // uppercase
		"gmail search",   // whitespace
		"gmail/../drive", // path traversal
		"",               // empty
	}
	for _, id := range rejected {
		if pluginIDRe.MatchString(id) {
			t.Errorf("pluginIDRe accepted plugin_id %q; want reject", id)
		}
	}

	accepted := []string{"gmail", "google-flights", "test-plugin", "a"}
	for _, id := range accepted {
		if !pluginIDRe.MatchString(id) {
			t.Errorf("pluginIDRe rejected plugin_id %q; want accept", id)
		}
	}
}

// TestLoadManifestRejectsDottedPluginID proves the regexp is wired into the
// manifest gate both install paths share, not merely declared. A table on
// pluginIDRe alone would still pass if LoadManifest stopped calling it.
func TestLoadManifestRejectsDottedPluginID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "executable"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(pluginID string) {
		t.Helper()
		manifest := fmt.Sprintf(`{
  "manifest_schema_version": 1,
  "plugin_id": %q,
  "name": "Namespace Probe",
  "version": "0.1.0",
  "shape": "mcp-plugin",
  "executable": "executable",
  "advertised_tools": [{"name": "search", "description": "probe", "risk_class": "read"}],
  "declared_capabilities": {"network": false, "fs_write_dir": "", "env_allow": []}
}`, pluginID)
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("gmail.search")
	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("LoadManifest accepted plugin_id \"gmail.search\"; want ErrManifestInvalid")
	}

	// Control: the same manifest with an undotted id loads, so the rejection
	// above came from the plugin_id and not from an unrelated field.
	write("gmail")
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("control LoadManifest(plugin_id=gmail) failed: %v", err)
	}
	if got := pluginOpID(m.PluginID, m.AdvertisedTools[0].Name); got != "plug.gmail.search" {
		t.Errorf("op_id = %q; want plug.gmail.search", got)
	}
}

// TestEmbeddedCatalogLeavesPlugNamespaceFree pins the first-party side. The
// separation holds only while no shipped op_id sits under "plug.": one that
// did could be shadowed by, or could shadow, an installed plugin's row in
// plugin-catalog.json.
func TestEmbeddedCatalogLeavesPlugNamespaceFree(t *testing.T) {
	var catalogDoc struct {
		Ops []struct {
			OpID string `json:"op_id"`
		} `json:"ops"`
	}
	if err := json.Unmarshal(embedded.CatalogJSON, &catalogDoc); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	if len(catalogDoc.Ops) == 0 {
		t.Fatal("embedded catalog has no ops; the guard below would be vacuous")
	}

	for _, op := range catalogDoc.Ops {
		if op.OpID == "" {
			t.Error("embedded catalog holds an op with an empty op_id")
			continue
		}
		if strings.HasPrefix(op.OpID, pluginNamespacePrefix) || op.OpID == strings.TrimSuffix(pluginNamespacePrefix, ".") {
			t.Errorf("embedded catalog op %q sits in the plugin namespace %q", op.OpID, pluginNamespacePrefix)
		}
	}
}
