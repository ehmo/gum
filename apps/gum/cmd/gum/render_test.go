package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// parseJSON is a test helper: unmarshal into `any` so the renderer sees the
// same float64/map[string]any shapes it gets from a real StructuredContent.
func parseJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	return v
}

func renderToString(t *testing.T, format, jsonBody string) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderStructured(&b, format, parseJSON(t, jsonBody)); err != nil {
		t.Fatalf("renderStructured(%s): %v", format, err)
	}
	return b.String()
}

// TestCLIFormatRouting pins which formats render in the CLI vs pass through to
// the dispatch encoder.
func TestCLIFormatRouting(t *testing.T) {
	for _, f := range []string{"table", "csv", "markdown"} {
		if !cliFormatNeedsStructured(f) {
			t.Errorf("%s should be CLI-rendered", f)
		}
	}
	for _, f := range []string{"json", "toon", "raw"} {
		if cliFormatNeedsStructured(f) {
			t.Errorf("%s should pass through to dispatch, not be CLI-rendered", f)
		}
	}
	for _, f := range []string{"table", "json", "toon", "csv", "markdown", "raw"} {
		if !validCLIFormat(f) {
			t.Errorf("%s should be a valid CLI format", f)
		}
	}
	if validCLIFormat("yaml") {
		t.Error("yaml should not be a valid CLI format")
	}
}

// TestRenderValueExtractsPaths pins the value(<path>) scripting format: a
// single field, an indexed array element, nested index, and a [] fan-out.
func TestRenderValueExtractsPaths(t *testing.T) {
	analytics := `{"responseAggregationType":"byProperty","rows":[{"clicks":53,"keys":["rasty turek"]},{"clicks":4,"keys":["rasto turek"]}]}`
	sites := `{"siteEntry":[{"siteUrl":"sc-domain:turek.co"},{"siteUrl":"sc-domain:gethasp.com"}]}`

	if got := strings.TrimSpace(renderValueToString(t, "responseAggregationType", analytics)); got != "byProperty" {
		t.Errorf("value(responseAggregationType) = %q, want byProperty", got)
	}
	if got := strings.TrimSpace(renderValueToString(t, "rows[0].clicks", analytics)); got != "53" {
		t.Errorf("value(rows[0].clicks) = %q, want 53", got)
	}
	if got := strings.TrimSpace(renderValueToString(t, "rows[1].keys[0]", analytics)); got != "rasto turek" {
		t.Errorf("value(rows[1].keys[0]) = %q, want rasto turek", got)
	}
	got := strings.Split(strings.TrimSpace(renderValueToString(t, "siteEntry[].siteUrl", sites)), "\n")
	want := []string{"sc-domain:turek.co", "sc-domain:gethasp.com"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("value(siteEntry[].siteUrl) = %v, want %v", got, want)
	}
	if got := renderValueToString(t, "nope.missing", sites); got != "" {
		t.Errorf("missing path produced %q, want empty", got)
	}
	if !cliFormatNeedsStructured("value(rows[0].clicks)") || !validCLIFormat("value(x)") {
		t.Error("value(...) should be a CLI-rendered valid format")
	}
}

func renderValueToString(t *testing.T, path, jsonBody string) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderStructured(&b, "value("+path+")", parseJSON(t, jsonBody)); err != nil {
		t.Fatalf("renderStructured value(%s): %v", path, err)
	}
	return b.String()
}

// TestResolveCallFormatEnvDefault pins that GUM_DEFAULT_OUTPUT supplies the
// default when no flag is set, an explicit flag overrides it, and an invalid
// env value is ignored (falls back to the TTY-aware default → json on a buffer).
func TestResolveCallFormatEnvDefault(t *testing.T) {
	var buf bytes.Buffer // non-TTY

	t.Setenv("GUM_DEFAULT_OUTPUT", "csv")
	if got, err := resolveCallFormat(&buf, "", false, false, false, false); err != nil || got != "csv" {
		t.Errorf("env default = (%q,%v), want (csv,nil)", got, err)
	}
	if got, _ := resolveCallFormat(&buf, "markdown", false, false, false, false); got != "markdown" {
		t.Errorf("explicit --output should override env, got %q", got)
	}
	t.Setenv("GUM_DEFAULT_OUTPUT", "yaml")
	if got, _ := resolveCallFormat(&buf, "", false, false, false, false); got != "json" {
		t.Errorf("invalid env value should fall back to json on non-TTY, got %q", got)
	}
	t.Setenv("GUM_DEFAULT_OUTPUT", "value(siteUrl)")
	if got, _ := resolveCallFormat(&buf, "", false, false, false, false); got != "value(siteUrl)" {
		t.Errorf("value() env default = %q, want value(siteUrl) (case preserved)", got)
	}
}

// TestResolveCallFormat pins gum call's format resolution: explicit --output or
// a format boolean wins; nothing explicit on a non-TTY writer (a bytes.Buffer)
// defaults to json (the piped/agent path); conflicts and unknown formats error.
func TestResolveCallFormat(t *testing.T) {
	t.Setenv("GUM_DEFAULT_OUTPUT", "") // hermetic: ignore any ambient default
	var buf bytes.Buffer               // non-TTY

	// No explicit format on a non-TTY → json (scripts/agents keep stable JSON).
	if got, err := resolveCallFormat(&buf, "", false, false, false, false); err != nil || got != "json" {
		t.Errorf("default non-TTY = (%q,%v), want (json,nil)", got, err)
	}
	// Explicit --output values.
	for _, f := range []string{"table", "csv", "markdown", "json", "toon"} {
		if got, err := resolveCallFormat(&buf, f, false, false, false, false); err != nil || got != f {
			t.Errorf("--output %s = (%q,%v), want (%s,nil)", f, got, err, f)
		}
	}
	// Format booleans.
	if got, _ := resolveCallFormat(&buf, "", true, false, false, false); got != "json" {
		t.Errorf("--json = %q, want json", got)
	}
	if got, _ := resolveCallFormat(&buf, "", false, false, true, false); got != "csv" {
		t.Errorf("--csv = %q, want csv", got)
	}
	// Conflict: --output and a boolean both set.
	if _, err := resolveCallFormat(&buf, "table", true, false, false, false); err == nil {
		t.Error("--output table + --json should be CLI_ARG_DUPLICATE")
	}
	// Conflict: two booleans.
	if _, err := resolveCallFormat(&buf, "", true, true, false, false); err == nil {
		t.Error("--json + --toon should be CLI_ARG_DUPLICATE")
	}
	// Unknown format.
	if _, err := resolveCallFormat(&buf, "yaml", false, false, false, false); err == nil {
		t.Error("--output yaml should error")
	}
}

// TestRenderStructuredFormats pins what the CLI wrapper emits for each
// CLI-rendered format. The wrapper is the only path `gum call --output table`
// takes, so a change in the render package that breaks the table or csv shape
// must fail here and not only in the render package's own tests.
func TestRenderStructuredFormats(t *testing.T) {
	rows := `{"rows":[{"clicks":53,"page":"/a"},{"clicks":4,"page":"/b"}]}`

	table := renderToString(t, "table", rows)
	for _, want := range []string{"clicks", "page", "53", "/a", "4", "/b"} {
		if !strings.Contains(table, want) {
			t.Errorf("table output missing %q:\n%s", want, table)
		}
	}

	csv := renderToString(t, "csv", rows)
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 3 {
		t.Errorf("csv should be a header plus 2 rows, got %d lines:\n%s", len(lines), csv)
	}
	if !strings.Contains(lines[0], ",") {
		t.Errorf("csv header is not comma separated: %q", lines[0])
	}

	md := renderToString(t, "markdown", rows)
	if !strings.Contains(md, "|") || !strings.Contains(md, "---") {
		t.Errorf("markdown output is not a pipe table:\n%s", md)
	}

	// An unknown format is not an error here: the caller gates the format and
	// the renderer falls back to indented JSON rather than dropping output.
	fallback := renderToString(t, "yaml", rows)
	if !strings.Contains(fallback, `"clicks": 53`) {
		t.Errorf("unknown format should fall back to indented JSON:\n%s", fallback)
	}
}
