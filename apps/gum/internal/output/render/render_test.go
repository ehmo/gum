package render

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
	if err := Structured(&b, format, parseJSON(t, jsonBody)); err != nil {
		t.Fatalf("Structured(%s): %v", format, err)
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

// TestPrimaryArrayKeyPrefersWellKnown pins finding #27: a response with several
// arrays tables the conventional data key (rows/items/...), not the
// alphabetically-first one.
func TestPrimaryArrayKeyPrefersWellKnown(t *testing.T) {
	if got := primaryArrayKey([]string{"columnHeaders", "rows"}); got != "rows" {
		t.Errorf("primaryArrayKey([columnHeaders rows]) = %q, want rows", got)
	}
	if got := primaryArrayKey([]string{"alpha", "beta"}); got != "alpha" {
		t.Errorf("primaryArrayKey (no well-known) = %q, want alpha (first)", got)
	}
	if got := primaryArrayKey([]string{"data", "items"}); got != "items" {
		t.Errorf("primaryArrayKey = %q, want items (items ranks above data)", got)
	}
}

// TestObjectViewPrefersRowsArray pins #27 through objectView itself: the rows
// array becomes the primary table even though columnHeaders sorts first.
func TestObjectViewPrefersRowsArray(t *testing.T) {
	obj := map[string]any{
		"columnHeaders": []any{map[string]any{"name": "query"}},
		"rows":          []any{map[string]any{"clicks": float64(5)}},
	}
	v := objectView(obj)
	found := false
	for _, c := range v.cols {
		if c == "clicks" {
			found = true
		}
	}
	if !found {
		t.Errorf("objectView cols = %v, want the rows table (clicks column)", v.cols)
	}
}

// TestNeutralizeCSVCellLeadingWhitespace pins finding #15: a formula trigger
// hidden behind leading whitespace is still defused (LibreOffice trims first).
func TestNeutralizeCSVCellLeadingWhitespace(t *testing.T) {
	cases := map[string]string{
		" =SUM(A1:A2)": "' =SUM(A1:A2)",
		"\t=cmd":       "'\t=cmd",
		"\n=cmd":       "'\n=cmd",
		"\r=cmd":       "'\r=cmd",
		"=evil":        "'=evil",
		"safe":         "safe",
		"  ":           "  ",
		"":             "",
	}
	for in, want := range cases {
		if got := neutralizeCSVCell(in); got != want {
			t.Errorf("neutralizeCSVCell(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestWriteMarkdownTableEscapesCR pins finding #11: a carriage return in a cell
// is replaced rather than emitted verbatim (it corrupts terminal output).
func TestWriteMarkdownTableEscapesCR(t *testing.T) {
	var b strings.Builder
	if err := writeMarkdownTable(&b, []string{"col"}, [][]string{{"a\rb"}}); err != nil {
		t.Fatalf("writeMarkdownTable: %v", err)
	}
	if strings.Contains(b.String(), "\r") {
		t.Errorf("markdown output contains a raw CR: %q", b.String())
	}
}
