package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// runCatalogListOverrides runs the command with stdout and stderr captured
// separately. The shared runCLI helper merges them, which would hide the one
// property this file tests: a refused row must not corrupt the NDJSON stream
// scripts read from stdout.
func runListOverridesSplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	var out, errBuf strings.Builder
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)

	err = root.Execute()
	return out.String(), errBuf.String(), err
}

// TestCatalogListOverridesSkipsPoisonedReason is the §5.4 denylist on the one
// path that reads plugin-catalog.json without going through
// catalog.MergePluginVariants. The JSON encoder here runs with
// SetEscapeHTML(false), so a bidi override in the reason reaches the terminal
// as written and reorders what the operator reads.
func TestCatalogListOverridesSkipsPoisonedReason(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	// U+202E RIGHT-TO-LEFT OVERRIDE inside an otherwise ordinary reason.
	body := []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    {
      "variant_id": "evil.v1.plugin.search",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "read-only\u202esetupmorf etirw"
    },
    {
      "variant_id": "good.v1.plugin.search",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "POST endpoint returning read-only results"
    }
  ]
}`)
	writePluginCatalog(t, dataRoot, "default", body)

	stdout, stderr, err := runListOverridesSplit(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("gum catalog list-overrides: %v (stderr: %q)", err, stderr)
	}

	lines := nonEmptyLines(strings.TrimSpace(stdout))
	if len(lines) != 1 {
		t.Fatalf("stdout has %d lines, want 1 (the clean row): %q", len(lines), stdout)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("stdout line is not JSON: %v", err)
	}
	if parsed["variant_id"] != "good.v1.plugin.search" {
		t.Fatalf("stdout carries %v; want only the clean row", parsed["variant_id"])
	}
	if strings.Contains(stdout, "\u202e") {
		t.Fatal("the bidi override reached stdout")
	}

	if !strings.Contains(stderr, "RISK_OVERRIDE_REASON_INVALID") {
		t.Fatalf("stderr does not report the code: %q", stderr)
	}
	if !strings.Contains(stderr, "evil.v1.plugin.search") {
		t.Fatalf("stderr does not name the refused variant: %q", stderr)
	}
}

// TestCatalogListOverridesPoisonedReasonDoesNotUnmaskEmbedded pins the
// precedence arm. A plugin row that shadows an embedded override and then
// fails the denylist must not leave the embedded reason in place: the plugin
// has claimed the variant_id, and printing the embedded reason under a
// variant the plugin now owns would misattribute it.
func TestCatalogListOverridesPoisonedReasonDoesNotUnmaskEmbedded(t *testing.T) {
	withTempConfigRootCLI(t)
	dataRoot := withTempDataRootCLI(t)

	body := []byte(`{
  "plugin_catalog_schema_version": 1,
  "variants": [
    {
      "variant_id": "evil.v1.plugin.search",
      "variant_schema_version": 1,
      "risk_class": "read",
      "risk_override": true,
      "risk_override_reason": "read-only <script>alert(1)</script>"
    }
  ]
}`)
	writePluginCatalog(t, dataRoot, "default", body)

	stdout, stderr, err := runListOverridesSplit(t, "catalog", "list-overrides")
	if err != nil {
		t.Fatalf("gum catalog list-overrides: %v (stderr: %q)", err, stderr)
	}
	if strings.Contains(stdout, "evil.v1.plugin.search") {
		t.Fatalf("refused row reached stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "RISK_OVERRIDE_REASON_INVALID") {
		t.Fatalf("stderr does not report the code: %q", stderr)
	}
}
