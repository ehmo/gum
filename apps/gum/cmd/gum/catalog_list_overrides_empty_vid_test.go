package main

import (
	"strings"
	"testing"
)

// TestCatalogListOverridesEmptyVariantIDSkipped pins catalog.go:104-105
// — `vid == "" → continue`. A plugin-catalog.json entry without a
// variant_id (or with variant_id set to empty string) MUST be silently
// skipped: it has no merge key, so writing it to `merged[""]` would
// pollute the output with a synthetic empty-keyed override entry.
// The companion well-formed entry must still emit.
func TestCatalogListOverridesEmptyVariantIDSkipped(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	body := []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    {
      "variant_id": "",
      "risk_class": "write",
      "risk_override": true,
      "risk_override_reason": "should be skipped — empty variant_id"
    },
    {
      "variant_id": "kept.entry.v1",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "kept companion"
    }
  ]
}`)
	writePluginCatalog(t, dataRoot, "default", body)

	out, err := runCLI(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("runCLI: %v", err)
	}
	if !strings.Contains(out, "kept.entry.v1") {
		t.Errorf("stdout=%q; want kept.entry.v1 line", out)
	}
	if strings.Contains(out, "should be skipped") {
		t.Errorf("stdout=%q; empty-variant_id entry must be skipped", out)
	}
}
