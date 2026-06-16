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

// TestRenderTableObjectWrappingArray pins the common Google list shape
// {"siteEntry":[...]} -> a table of the wrapped array with sorted columns.
func TestRenderTableObjectWrappingArray(t *testing.T) {
	out := renderToString(t, "table",
		`{"siteEntry":[{"permissionLevel":"siteOwner","siteUrl":"sc-domain:turek.co"},{"permissionLevel":"siteUser","siteUrl":"sc-domain:gethasp.com"}]}`)
	// Columns are the union of record keys, sorted: permissionLevel, siteUrl.
	if !strings.Contains(out, "permissionLevel") || !strings.Contains(out, "siteUrl") {
		t.Errorf("missing headers in:\n%s", out)
	}
	if strings.Index(out, "permissionLevel") > strings.Index(out, "siteUrl") {
		t.Errorf("columns not sorted (permissionLevel should precede siteUrl):\n%s", out)
	}
	for _, want := range []string{"siteOwner", "sc-domain:turek.co", "siteUser", "sc-domain:gethasp.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing cell %q in:\n%s", want, out)
		}
	}
}

// TestRenderTableScalarSiblingsPlusArray pins the searchanalytics shape:
// scalar siblings render as lead lines, the array renders as a table, and a
// nested array cell (keys) becomes compact JSON.
func TestRenderTableScalarSiblingsPlusArray(t *testing.T) {
	out := renderToString(t, "table",
		`{"responseAggregationType":"byProperty","rows":[{"clicks":53,"impressions":268,"keys":["rasty turek"]}]}`)
	if !strings.Contains(out, "responseAggregationType: byProperty") {
		t.Errorf("scalar sibling not rendered as lead line:\n%s", out)
	}
	if !strings.Contains(out, "clicks") || !strings.Contains(out, "53") || !strings.Contains(out, "268") {
		t.Errorf("array not tabulated:\n%s", out)
	}
	if !strings.Contains(out, `["rasty turek"]`) {
		t.Errorf("nested array cell not compact JSON:\n%s", out)
	}
}

// TestRenderTableFlatObject pins that a flat object becomes a field/value table.
func TestRenderTableFlatObject(t *testing.T) {
	out := renderToString(t, "table", `{"name":"turek.co","verified":true}`)
	if !strings.Contains(out, "field") || !strings.Contains(out, "value") {
		t.Errorf("flat object should render field/value headers:\n%s", out)
	}
	if !strings.Contains(out, "name") || !strings.Contains(out, "turek.co") || !strings.Contains(out, "verified") || !strings.Contains(out, "true") {
		t.Errorf("flat object rows missing:\n%s", out)
	}
}

// TestRenderMarkdownTable pins GitHub-flavored Markdown output.
func TestRenderMarkdownTable(t *testing.T) {
	out := renderToString(t, "markdown", `[{"a":1,"b":"x"},{"a":2,"b":"y"}]`)
	if !strings.Contains(out, "| a | b |") {
		t.Errorf("missing markdown header row:\n%s", out)
	}
	if !strings.Contains(out, "| --- | --- |") {
		t.Errorf("missing markdown separator row:\n%s", out)
	}
	if !strings.Contains(out, "| 1 | x |") || !strings.Contains(out, "| 2 | y |") {
		t.Errorf("missing markdown data rows:\n%s", out)
	}
}

// TestRenderCSV pins CSV output (header + rows) for an array of objects.
func TestRenderCSV(t *testing.T) {
	out := renderToString(t, "csv", `{"siteEntry":[{"permissionLevel":"siteOwner","siteUrl":"sc-domain:turek.co"}]}`)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header + 1 row, got %d lines:\n%s", len(lines), out)
	}
	if lines[0] != "permissionLevel,siteUrl" {
		t.Errorf("CSV header = %q, want permissionLevel,siteUrl", lines[0])
	}
	if lines[1] != "siteOwner,sc-domain:turek.co" {
		t.Errorf("CSV row = %q", lines[1])
	}
}

// TestRenderTableArrayOfScalars pins that an array of scalars renders under a
// single "value" column rather than erroring.
func TestRenderTableArrayOfScalars(t *testing.T) {
	out := renderToString(t, "table", `["a","b","c"]`)
	if !strings.Contains(out, "value") || !strings.Contains(out, "a") || !strings.Contains(out, "c") {
		t.Errorf("array-of-scalars not rendered:\n%s", out)
	}
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

// TestRenderTableEmptyArrayWithSiblings pins that an empty primary array shown
// alongside scalar siblings still reports "(no rows)" rather than silently
// printing only the scalars.
func TestRenderTableEmptyArrayWithSiblings(t *testing.T) {
	out := renderToString(t, "table", `{"responseAggregationType":"byProperty","rows":[]}`)
	if !strings.Contains(out, "responseAggregationType: byProperty") {
		t.Errorf("scalar sibling missing:\n%s", out)
	}
	if !strings.Contains(out, "(no rows)") {
		t.Errorf("empty array should report (no rows):\n%s", out)
	}
}

// TestRenderCSVNeutralizesFormula pins CSV formula-injection defusing: a cell
// starting with = (or +,-,@) is prefixed with a single quote.
func TestRenderCSVNeutralizesFormula(t *testing.T) {
	out := renderToString(t, "csv", `{"rows":[{"name":"=SUM(A1:A9)"}]}`)
	if !strings.Contains(out, "'=SUM(A1:A9)") {
		t.Errorf("formula cell not neutralized:\n%s", out)
	}
}

// TestDisplayWidthASCII pins that plain ASCII runes each count as 1 column.
func TestDisplayWidthASCII(t *testing.T) {
	for _, r := range "hello" {
		if got := displayWidth(r); got != 1 {
			t.Errorf("displayWidth(%q) = %d, want 1", r, got)
		}
	}
	if got := stringDisplayWidth("hello"); got != 5 {
		t.Errorf("stringDisplayWidth(hello) = %d, want 5", got)
	}
}

// TestDisplayWidthCJK pins that CJK / Hiragana / Hangul runes count as 2 columns.
func TestDisplayWidthCJK(t *testing.T) {
	wide := []rune{'中', 0x3042 /* あ */, 0xD55C /* 한 */, 0xFF21 /* fullwidth A */, 0x33C0 /* CJK unit symbol — was a wideRanges gap */, 0x33FE /* CJK Compatibility tail */}
	for _, r := range wide {
		if got := displayWidth(r); got != 2 {
			t.Errorf("displayWidth(U+%04X) = %d, want 2", r, got)
		}
	}
	if got := stringDisplayWidth("中文字"); got != 6 {
		t.Errorf("stringDisplayWidth(中文字) = %d, want 6", got)
	}
}

// TestDisplayWidthZeroWidth pins that combining marks and explicit zero-width
// codepoints contribute 0 columns.
func TestDisplayWidthZeroWidth(t *testing.T) {
	for _, r := range []rune{0x0301 /* combining acute, Mn */, 0x200B /* ZWSP */, 0x200D /* ZWJ */} {
		if got := displayWidth(r); got != 0 {
			t.Errorf("displayWidth(U+%04X) = %d, want 0", r, got)
		}
	}
}

// TestWideRangesSorted pins the invariant the early-break in displayWidth relies
// on: wideRanges is sorted by lo and each range is well-formed (lo <= hi).
func TestWideRangesSorted(t *testing.T) {
	for i, rng := range wideRanges {
		if rng[0] > rng[1] {
			t.Errorf("wideRanges[%d] = [%#x,%#x] is inverted", i, rng[0], rng[1])
		}
		if i > 0 && rng[0] <= wideRanges[i-1][1] {
			t.Errorf("wideRanges[%d] lo %#x not strictly after prev hi %#x (unsorted/overlapping)", i, rng[0], wideRanges[i-1][1])
		}
	}
}

// TestASCIITableCJKColumnWidth pins that a CJK header produces a column wide
// enough for its 2-column glyphs and that padding arithmetic stays non-negative.
func TestASCIITableCJKColumnWidth(t *testing.T) {
	var buf bytes.Buffer
	// header "名前" = 4 display cols; data "Alice" = 5 cols -> column width 5.
	if err := writeASCIITable(&buf, []string{"名前"}, [][]string{{"Alice"}}); err != nil {
		t.Fatalf("writeASCIITable: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "名前") {
		t.Errorf("CJK header missing:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("too few lines:\n%s", out)
	}
	if !strings.HasPrefix(lines[1], "-----") {
		t.Errorf("separator too short (want >=5 dashes from the 5-col cell): %q", lines[1])
	}
}

// TestTruncateCellWideRune pins that truncation never splits a wide rune and the
// result display-width never exceeds maxCellWidth, and that an exactly-fitting
// ASCII string is returned unchanged.
func TestTruncateCellWideRune(t *testing.T) {
	s := strings.Repeat("中", 31) // 62 display cols > maxCellWidth (60)
	got := truncateCell(s)
	if stringDisplayWidth(got) > maxCellWidth {
		t.Errorf("truncateCell width %d > maxCellWidth %d: %q", stringDisplayWidth(got), maxCellWidth, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncateCell did not append ellipsis: %q", got)
	}
	exact := strings.Repeat("a", maxCellWidth)
	if got2 := truncateCell(exact); got2 != exact {
		t.Errorf("truncateCell modified a string of exactly maxCellWidth ASCII chars")
	}
}
