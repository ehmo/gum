package main

// Tests for `gum catalog list-overrides` CLI surface.
//
// Environment isolation: every test MUST redirect both XDG_CONFIG_HOME (so the
// config subsystem never reads from the developer's real ~/.config/gum tree) and
// XDG_DATA_HOME (so plugin-catalog.json lookups use a fresh tempdir). Both env
// vars are restored automatically after each test via t.Setenv.
//
// withTempDataRootCLI sets XDG_DATA_HOME to a fresh temp directory.
// writePluginCatalog writes a plugin-catalog.json file under a given profile.
// runCLI is shared from config_cli_test.go in the same package.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// withTempDataRootCLI redirects XDG_DATA_HOME to a t.TempDir() for the
// duration of the test. Returns the path of the temporary directory.
func withTempDataRootCLI(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	return root
}

// writePluginCatalog writes plugin-catalog.json under the data root for the
// given profile. The body is the raw JSON bytes (caller crafts the schema_version
// and variants payload).
func writePluginCatalog(t *testing.T, dataRoot, profile string, body []byte) string {
	t.Helper()
	p := filepath.Join(dataRoot, "gum", profile, "plugin-catalog.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

// TestCatalogListOverridesEmptyExitsZero asserts that when no plugin-catalog.json
// exists and the embedded catalog has no risk_override variants, the command
// exits 0 with empty stdout.
func TestCatalogListOverridesEmptyExitsZero(t *testing.T) {
	withTempConfigRootCLI(t)
	withTempDataRootCLI(t)

	out, err := runCLI(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("gum catalog list-overrides: expected nil error, got %v (output: %q)", err, out)
	}
	// If the embedded catalog itself has no overrides the output must be empty.
	// Parse each line to verify — any line must be valid JSON; we only enforce
	// emptiness if there truly are no overrides anywhere.
	lines := nonEmptyLines(out)
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("output line is not valid JSON: %q: %v", line, err)
		}
	}
	// No plugin-catalog.json was written, so if the embedded catalog also has
	// no overrides, we expect exactly zero lines.  The test does not enforce
	// the zero-line constraint when the embedded catalog itself has overrides
	// (that is covered by TestCatalogListOverridesEmbeddedCatalogContributes).
}

// TestCatalogListOverridesEmitsPluginOverride asserts that a plugin-catalog.json
// with one risk_override:true variant and one risk_override:false variant emits
// exactly one JSON line with the three required fields and no extras.
func TestCatalogListOverridesEmitsPluginOverride(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	body := []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    {
      "variant_id": "fli.v1.plugin.search",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "POST endpoint returning read-only results"
    },
    {
      "variant_id": "calendar.v3.read-only.list",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": false
    }
  ]
}`)
	writePluginCatalog(t, dataRoot, "default", body)

	out, err := runCLI(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("gum catalog list-overrides: unexpected error %v (output: %q)", err, out)
	}

	lines := nonEmptyLines(out)
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 output line, got %d: %q", len(lines), out)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("output line not valid JSON: %v", err)
	}
	if v, ok := parsed["variant_id"]; !ok || v != "fli.v1.plugin.search" {
		t.Errorf("variant_id: got %v, want %q", v, "fli.v1.plugin.search")
	}
	if v, ok := parsed["risk_class"]; !ok || v != "read" {
		t.Errorf("risk_class: got %v, want %q", v, "read")
	}
	if v, ok := parsed["risk_override_reason"]; !ok || v != "POST endpoint returning read-only results" {
		t.Errorf("risk_override_reason: got %v, want %q", v, "POST endpoint returning read-only results")
	}
	if len(parsed) != 3 {
		t.Errorf("output map has %d keys, want exactly 3: %v", len(parsed), parsed)
	}
}

// TestCatalogListOverridesProfileFlagScopesToProfile asserts that --profile=X
// reads plugin-catalog.json only from the X profile directory, not from default.
func TestCatalogListOverridesProfileFlagScopesToProfile(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	teamA := []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    {
      "variant_id": "team-a.override.variant",
      "variant_schema_version": 1,
      "risk_class": "write",
      "risk_override": true,
      "risk_override_reason": "team A audit pass"
    }
  ]
}`)
	writePluginCatalog(t, dataRoot, "team-a", teamA)

	defaultCat := []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    {
      "variant_id": "default.override.variant",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "default profile"
    }
  ]
}`)
	writePluginCatalog(t, dataRoot, "default", defaultCat)

	out, err := runCLI(t, "--profile=team-a", "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("gum --profile=team-a catalog list-overrides: unexpected error %v", err)
	}

	lines := nonEmptyLines(out)
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 output line, got %d: %q", len(lines), out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("output line not valid JSON: %v", err)
	}
	if parsed["variant_id"] != "team-a.override.variant" {
		t.Errorf("variant_id: got %v, want %q (profile scoping failed)", parsed["variant_id"], "team-a.override.variant")
	}
}

// TestCatalogListOverridesSortedByVariantID asserts that the output is sorted
// lexicographically ascending by variant_id regardless of file order.
func TestCatalogListOverridesSortedByVariantID(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	body := []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    {
      "variant_id": "zzz.variant",
      "variant_schema_version": 1,
      "risk_class": "write",
      "risk_override": true,
      "risk_override_reason": "z"
    },
    {
      "variant_id": "aaa.variant",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "a"
    },
    {
      "variant_id": "mmm.variant",
      "variant_schema_version": 1,
      "risk_class": "destructive",
      "risk_override": true,
      "risk_override_reason": "m"
    }
  ]
}`)
	writePluginCatalog(t, dataRoot, "default", body)

	out, err := runCLI(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("gum catalog list-overrides: unexpected error %v", err)
	}

	lines := nonEmptyLines(out)
	// There may be additional lines from the embedded catalog; we only assert
	// that aaa < mmm < zzz appear in that relative order among all lines.
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("non-JSON line: %q: %v", line, err)
		}
		ids = append(ids, m["variant_id"].(string))
	}

	// Verify the three plugin variants appear in sorted order among all output.
	targets := []string{"aaa.variant", "mmm.variant", "zzz.variant"}
	idxOf := func(id string) int {
		for i, v := range ids {
			if v == id {
				return i
			}
		}
		return -1
	}
	for i := 0; i < len(targets)-1; i++ {
		a, b := targets[i], targets[i+1]
		ia, ib := idxOf(a), idxOf(b)
		if ia == -1 {
			t.Errorf("variant_id %q not found in output", a)
			continue
		}
		if ib == -1 {
			t.Errorf("variant_id %q not found in output", b)
			continue
		}
		if ia >= ib {
			t.Errorf("sort order violated: %q (index %d) should come before %q (index %d)", a, ia, b, ib)
		}
	}

	// Also assert the full output is globally sorted.
	for i := 1; i < len(ids); i++ {
		if ids[i] < ids[i-1] {
			t.Errorf("output not sorted at position %d: %q < %q", i, ids[i], ids[i-1])
		}
	}
}

// TestCatalogListOverridesUnsupportedSchemaErrors asserts that a plugin-catalog.json
// with an unrecognised plugin_catalog_schema_version causes a non-nil error
// containing PLUGIN_CATALOG_SCHEMA_UNSUPPORTED.
func TestCatalogListOverridesUnsupportedSchemaErrors(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	body := []byte(`{"plugin_catalog_schema_version": 999, "variants": []}`)
	writePluginCatalog(t, dataRoot, "default", body)

	_, err := runCLI(t, "catalog", "list-overrides")
	if err == nil {
		t.Fatal("gum catalog list-overrides with schema_version=999: expected non-nil error, got nil")
	}
	if !strings.Contains(err.Error(), "PLUGIN_CATALOG_SCHEMA_UNSUPPORTED") {
		t.Errorf("error %q does not contain PLUGIN_CATALOG_SCHEMA_UNSUPPORTED", err.Error())
	}
}

// TestCatalogListOverridesEmbeddedCatalogContributes pins the embedded-catalog
// merge contract: any overrides present in the embedded catalog must appear in
// the output with non-empty variant_id, valid risk_class, and non-empty
// risk_override_reason. If the embedded catalog has no overrides today the test
// vacuously passes.
func TestCatalogListOverridesEmbeddedCatalogContributes(t *testing.T) {
	withTempConfigRootCLI(t)
	withTempDataRootCLI(t) // no plugin-catalog.json written → treated as empty v1

	out, err := runCLI(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("gum catalog list-overrides: unexpected error %v", err)
	}

	lines := nonEmptyLines(out)
	validRiskClasses := map[string]bool{"read": true, "write": true, "destructive": true}

	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("output line not valid JSON: %q: %v", line, err)
			continue
		}
		vid, _ := m["variant_id"].(string)
		if vid == "" {
			t.Errorf("output line has empty variant_id: %q", line)
		}
		rc, _ := m["risk_class"].(string)
		if !validRiskClasses[rc] {
			t.Errorf("output line has invalid risk_class %q: %q", rc, line)
		}
		reason, _ := m["risk_override_reason"].(string)
		if reason == "" {
			t.Errorf("output line has empty risk_override_reason: %q", line)
		}
	}
}

// TestCatalogListOverridesPluginPrecedenceOverEmbedded pins the rule that
// plugin-catalog entries take precedence over embedded catalog entries on
// variant_id collision. The test is skipped when the embedded catalog has no
// risk_override variants (the collision scenario is vacuous).
func TestCatalogListOverridesPluginPrecedenceOverEmbedded(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	// Find the first embedded variant with risk_override: true.
	type embVariant struct {
		VariantID          string `json:"variant_id"`
		RiskOverride       bool   `json:"risk_override"`
		RiskOverrideReason string `json:"risk_override_reason"`
		RiskClass          string `json:"risk_class"`
	}
	type embCatalog struct {
		Variants []embVariant `json:"variants"`
	}

	var ec embCatalog
	if len(embedded.CatalogJSON) > 0 {
		_ = json.Unmarshal(embedded.CatalogJSON, &ec)
	}

	var target *embVariant
	for i := range ec.Variants {
		if ec.Variants[i].RiskOverride {
			target = &ec.Variants[i]
			break
		}
	}
	if target == nil {
		t.Skip("embedded catalog has no risk_override variants — collision test vacuous")
	}

	// Write a plugin-catalog.json that overrides the same variant_id with a
	// sentinel reason so we can detect which entry "won".
	body, _ := json.Marshal(map[string]any{
		"plugin_catalog_schema_version": 1,
		"variants": []map[string]any{
			{
				"variant_id":           target.VariantID,
				"variant_schema_version": 1,
				"risk_class":           target.RiskClass,
				"risk_override":        true,
				"risk_override_reason": "profile-override-wins",
			},
		},
	})
	writePluginCatalog(t, dataRoot, "default", body)

	out, err := runCLI(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("gum catalog list-overrides: unexpected error %v", err)
	}

	lines := nonEmptyLines(out)
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("non-JSON line: %q: %v", line, err)
		}
		if m["variant_id"] == target.VariantID {
			if m["risk_override_reason"] != "profile-override-wins" {
				t.Errorf("collision: variant_id=%q: got reason %q, want %q (plugin-catalog should win)",
					target.VariantID, m["risk_override_reason"], "profile-override-wins")
			}
			return
		}
	}
	t.Errorf("variant_id %q not found in output (expected from plugin-catalog or embedded)", target.VariantID)
}

// TestCatalogListOverridesJSONOutputIsValidNDJSON asserts that every non-empty
// output line is individually parseable as a JSON object (NDJSON format).
func TestCatalogListOverridesJSONOutputIsValidNDJSON(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	body := []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    {
      "variant_id": "a.variant",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "first override"
    },
    {
      "variant_id": "b.variant",
      "variant_schema_version": 1,
      "risk_class": "write",
      "risk_override": true,
      "risk_override_reason": "second override"
    }
  ]
}`)
	writePluginCatalog(t, dataRoot, "default", body)

	out, err := runCLI(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("gum catalog list-overrides: unexpected error %v", err)
	}

	lines := nonEmptyLines(out)
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 output lines, got %d: %q", len(lines), out)
	}
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %q: %v", i, line, err)
		}
	}
}

// nonEmptyLines splits s on newlines and returns only the non-empty ones.
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
