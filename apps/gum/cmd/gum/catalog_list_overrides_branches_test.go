package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// TestCatalogListOverridesEmbeddedRiskOverrideArm pins the inner
// `if v.RiskOverride` truthy branch when seeding from the embedded
// catalog. The real embedded catalog has no risk_override variants
// (so the production seed loop never enters this branch), but
// future builds MUST emit any flagged variant — swap in a synthetic
// catalog with exactly one such variant and assert it round-trips
// into stdout.
func TestCatalogListOverridesEmbeddedRiskOverrideArm(t *testing.T) {
	withTempConfigRootCLI(t)
	withTempDataRootCLI(t) // no plugin-catalog.json — embedded path only

	synthetic := []byte(`{
  "catalog_schema_version": 1,
  "ops": [
    {
      "op_id": "synthetic.op",
      "default_variant_id": "synthetic.op.v1",
      "variants": [
        {
          "variant_id": "synthetic.op.v1",
          "risk_class": "write",
          "risk_override": true,
          "risk_override_reason": "synthetic override for coverage"
        }
      ]
    }
  ]
}`)
	saved := embedded.CatalogJSON
	t.Cleanup(func() { embedded.CatalogJSON = saved })
	embedded.CatalogJSON = synthetic

	out, err := runCLI(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("runCLI: %v", err)
	}
	if !strings.Contains(out, "synthetic.op.v1") {
		t.Errorf("stdout=%q; want synthetic.op.v1 line emitted from embedded seed", out)
	}
	if !strings.Contains(out, "synthetic override for coverage") {
		t.Errorf("stdout=%q; want synthetic reason propagated", out)
	}
}

// TestCatalogListOverridesReadFileNonENOENTError pins the
// `err != nil && !errors.Is(err, os.ErrNotExist)` arm of the
// plugin-catalog.json read. Planting a directory at the
// plugin-catalog.json path makes ReadFile return EISDIR (which is
// NOT ENOENT), so the command MUST surface "reading plugin-catalog.json"
// rather than silently treat it as an empty catalog.
func TestCatalogListOverridesReadFileNonENOENTError(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	// Plant a *directory* at the plugin-catalog.json path so os.ReadFile
	// returns EISDIR, not ENOENT.
	dir := filepath.Join(dataRoot, "gum", "default", "plugin-catalog.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}

	_, err := runCLI(t, "catalog", "list-overrides")
	if err == nil {
		t.Fatal("want ReadFile EISDIR err to surface; got nil")
	}
	if !strings.Contains(err.Error(), "reading plugin-catalog.json") {
		t.Errorf("err=%v; want 'reading plugin-catalog.json' wrap", err)
	}
}

// TestCatalogListOverridesLoadPluginCatalogGenericError pins the
// `catalog.LoadPluginCatalog err is NOT ErrUnsupportedPluginCatalogSchemaVersion`
// fall-through arm. An invalid-JSON body fails parsing before any
// schema-version check, surfacing the generic "catalog list-overrides:"
// wrap rather than the PLUGIN_CATALOG_SCHEMA_UNSUPPORTED sentinel.
func TestCatalogListOverridesLoadPluginCatalogGenericError(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)
	writePluginCatalog(t, dataRoot, "default", []byte("{not json"))

	_, err := runCLI(t, "catalog", "list-overrides")
	if err == nil {
		t.Fatal("want generic LoadPluginCatalog parse err; got nil")
	}
	if strings.Contains(err.Error(), "PLUGIN_CATALOG_SCHEMA_UNSUPPORTED") {
		t.Errorf("err=%v; non-schema error must NOT use the SCHEMA_UNSUPPORTED sentinel", err)
	}
	if !strings.Contains(err.Error(), "catalog list-overrides") {
		t.Errorf("err=%v; want 'catalog list-overrides' wrap", err)
	}
}

// TestCatalogListOverridesVariantNotMapEntrySkipped pins the
// `m, ok := raw.(map[string]any); if !ok { continue }` arm: a
// plugin-catalog.json that smuggles a non-object Variants entry (a
// raw string) MUST be silently skipped rather than panic on the
// type assertion. The companion object-entry MUST still emit.
func TestCatalogListOverridesVariantNotMapEntrySkipped(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	// Mix a string entry with a valid object entry. LoadPluginCatalog
	// stores Variants as []any, so a string survives parse and only
	// gets filtered at the runtime type assertion.
	body := []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    "this-is-not-an-object",
    {
      "variant_id": "valid.v1.entry",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "still emitted"
    }
  ]
}`)
	writePluginCatalog(t, dataRoot, "default", body)

	out, err := runCLI(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("runCLI: %v", err)
	}
	if !strings.Contains(out, "valid.v1.entry") {
		t.Errorf("stdout=%q; want valid.v1.entry surviving the type-assertion filter", out)
	}
}
