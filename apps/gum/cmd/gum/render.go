package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// render.go is the CLI presentation layer for catalog results. The dispatch
// kernel only speaks the spec wire formats (raw|toon|json, §9); the human
// formats (table|csv|markdown) are rendered here from the parsed result, so the
// kernel contract and the MCP path stay untouched. The agent grammar
// (gum call <op_id> key=value body:=json) is unaffected — only how a result is
// printed to a terminal changes.

// maxCellWidth caps a single table cell so one long value can't smear the
// table across the screen; longer values are truncated with an ellipsis.
const maxCellWidth = 60

// wideRanges lists Unicode code-point ranges whose glyphs occupy 2 terminal
// columns (East-Asian wide / fullwidth). Ranges are [lo, hi] inclusive and
// sorted by lo for a fast linear scan with early break. Covers the blocks
// needed for correct alignment of CJK, Hangul, Hiragana, Katakana, fullwidth
// ASCII/punctuation, and the CJK compatibility/enclosed/emoji forms that also
// render double-width. Source: Unicode Standard Annex #11 (East Asian Width).
var wideRanges = [][2]rune{
	{0x1100, 0x115F},   // Hangul Jamo
	{0x2329, 0x232A},   // Angle brackets (CJK)
	{0x2E80, 0x303E},   // CJK Radicals … CJK Symbols and Punctuation (incl. U+3000)
	{0x3041, 0x33FF},   // Hiragana, Katakana, Bopomofo, Hangul Compat Jamo, Kanbun, CJK symbols, full CJK Compatibility block (incl. 0x33C0-0x33FE unit symbols)
	{0x3400, 0x4DBF},   // CJK Unified Ideographs Extension A
	{0x4E00, 0x9FFF},   // CJK Unified Ideographs
	{0xA000, 0xA4CF},   // Yi Syllables / Yi Radicals
	{0xA960, 0xA97F},   // Hangul Jamo Extended-A
	{0xAC00, 0xD7AF},   // Hangul Syllables
	{0xF900, 0xFAFF},   // CJK Compatibility Ideographs
	{0xFE10, 0xFE19},   // Vertical Forms
	{0xFE30, 0xFE4F},   // CJK Compatibility Forms
	{0xFE50, 0xFE6F},   // Small Form Variants
	{0xFF01, 0xFF60},   // Fullwidth Latin / CJK Halfwidth-and-Fullwidth Forms (wide half)
	{0xFFE0, 0xFFE6},   // Fullwidth currency / signs
	{0x1B000, 0x1B12F}, // Kana Supplement / Kana Extended-A
	{0x1F200, 0x1F2FF}, // Enclosed Ideographic Supplement
	{0x1F300, 0x1F64F}, // Misc Symbols/Pictographs, Emoticons
	{0x1F900, 0x1F9FF}, // Supplemental Symbols and Pictographs
	{0x20000, 0x2FFFD}, // CJK Unified Ideographs Extensions B–F + Compatibility Supplement
	{0x30000, 0x3134F}, // CJK Unified Ideographs Extension G
}

// displayWidth returns the number of terminal columns a single rune occupies:
// 2 for East-Asian wide/fullwidth runes, 0 for zero-width combining/format
// characters, and 1 for everything else.
func displayWidth(r rune) int {
	// Zero-width: explicit zero-width codepoints, then combining marks (Mn =
	// nonspacing, Me = enclosing) and format/control characters (Cf).
	if r == 0x200B || r == 0x200C || r == 0x200D || r == 0xFEFF {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	for _, rng := range wideRanges {
		if r < rng[0] {
			break // ranges are sorted by lo; no later range can match
		}
		if r <= rng[1] {
			return 2
		}
	}
	return 1
}

// stringDisplayWidth returns the total terminal column-width of s.
func stringDisplayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += displayWidth(r)
	}
	return w
}

// cliFormatNeedsStructured reports whether format is rendered here in the CLI
// from the structured result (true) versus passed straight through to the
// dispatch encoder (false, for json|toon|raw).
func cliFormatNeedsStructured(format string) bool {
	if _, ok := valuePath(format); ok {
		return true
	}
	switch format {
	case "table", "csv", "markdown":
		return true
	}
	return false
}

// validCLIFormat reports whether format is an accepted --output value.
func validCLIFormat(format string) bool {
	if _, ok := valuePath(format); ok {
		return true
	}
	switch format {
	case "table", "json", "toon", "csv", "markdown", "raw":
		return true
	}
	return false
}

// valuePath reports whether format is a value(<path>) selector and returns the
// inner path. The "value(" prefix is case-insensitive but the path is returned
// verbatim — JSON field names are case-sensitive.
func valuePath(format string) (string, bool) {
	if strings.HasPrefix(strings.ToLower(format), "value(") && strings.HasSuffix(format, ")") {
		return format[len("value(") : len(format)-1], true
	}
	return "", false
}

// renderStructured writes v (a parsed JSON result) to w in a human format.
func renderStructured(w io.Writer, format string, v any) error {
	if p, ok := valuePath(format); ok {
		return renderValue(w, p, v)
	}
	switch format {
	case "table":
		return renderTabular(w, v, false)
	case "markdown":
		return renderTabular(w, v, true)
	case "csv":
		return renderCSV(w, v)
	default:
		// Caller gates the format; fall back to indented JSON rather than panic.
		b, _ := json.MarshalIndent(v, "", "  ")
		_, err := fmt.Fprintln(w, string(b))
		return err
	}
}

// renderValue prints the scalar(s) at a dot/bracket path, one per line, with no
// headers — the gcloud-style format for shell scripting (e.g.
// QUERY=$(gum call ... --output 'value(rows[0].keys[0])')). A [] segment fans
// out to one line per element; a missing path prints nothing.
func renderValue(w io.Writer, path string, v any) error {
	for _, r := range extractPath(v, path) {
		if _, err := fmt.Fprintln(w, scalarString(r)); err != nil {
			return err
		}
	}
	return nil
}

// extractPath walks v along a dot path whose segments may carry an [N] index or
// an [] iterate accessor (e.g. "rows[0].clicks", "siteEntry[].siteUrl"),
// returning every matched value (fanned out across [] iterations). An empty
// path returns v itself.
func extractPath(v any, path string) []any {
	if strings.TrimSpace(path) == "" {
		return []any{v}
	}
	cur := []any{v}
	for _, seg := range strings.Split(path, ".") {
		var next []any
		for _, node := range cur {
			next = append(next, applySegment(node, seg)...)
		}
		cur = next
	}
	return cur
}

// applySegment resolves one path segment against node: an optional map key
// followed by an optional [N]/[] array accessor. A type mismatch or missing key
// yields no values (the path simply does not match).
func applySegment(node any, seg string) []any {
	key := seg
	bracket := ""
	hasBracket := false
	if i := strings.IndexByte(seg, '['); i >= 0 && strings.HasSuffix(seg, "]") {
		key = seg[:i]
		bracket = seg[i+1 : len(seg)-1]
		hasBracket = true
	}
	if key != "" {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		val, ok := m[key]
		if !ok {
			return nil
		}
		node = val
	}
	if !hasBracket {
		return []any{node}
	}
	arr, ok := node.([]any)
	if !ok {
		return nil
	}
	if bracket == "" {
		return append([]any{}, arr...)
	}
	idx, err := strconv.Atoi(bracket)
	if err != nil || idx < 0 || idx >= len(arr) {
		return nil
	}
	return []any{arr[idx]}
}

// resultView is a flattened, render-ready view of a parsed result: a set of
// leading scalar fields (printed as `key: value` lines above a table) plus a
// primary record set (cols + rows) to tabulate.
type resultView struct {
	leadKeys []string          // sorted scalar field names
	lead     map[string]string // scalar field -> rendered value
	cols     []string          // table column headers
	rows     [][]string        // table rows aligned to cols
	notes    []string          // e.g. "other_array: [N items]"
}

// buildView derives a resultView from a parsed JSON value, handling the common
// Google API result shapes: an object wrapping a single array (e.g.
// {"siteEntry":[...]}), an object with scalar siblings plus one array (e.g.
// {"responseAggregationType":"byProperty","rows":[...]}), a bare array of
// objects, a flat object (rendered key/value), or a scalar.
func buildView(v any) resultView {
	switch t := v.(type) {
	case []any:
		cols, rows := recordsTable(t)
		return resultView{cols: cols, rows: rows}
	case map[string]any:
		return objectView(t)
	default:
		return resultView{cols: []string{"value"}, rows: [][]string{{scalarString(v)}}}
	}
}

// objectView splits an object into scalar fields and array fields, tabulating
// the first array-of-objects (or array) it finds and listing scalars as lead
// lines. A flat object (no arrays) is rendered as a field/value table.
func objectView(obj map[string]any) resultView {
	var scalarKeys, arrayKeys []string
	for k, val := range obj {
		if _, isArr := val.([]any); isArr {
			arrayKeys = append(arrayKeys, k)
		} else {
			scalarKeys = append(scalarKeys, k)
		}
	}
	sort.Strings(scalarKeys)
	sort.Strings(arrayKeys)

	if len(arrayKeys) == 0 {
		// No array: render the object itself as a field/value table.
		rows := make([][]string, 0, len(scalarKeys))
		for _, k := range scalarKeys {
			rows = append(rows, []string{k, scalarString(obj[k])})
		}
		return resultView{cols: []string{"field", "value"}, rows: rows}
	}

	view := resultView{lead: map[string]string{}}
	for _, k := range scalarKeys {
		view.leadKeys = append(view.leadKeys, k)
		view.lead[k] = scalarString(obj[k])
	}
	primary := primaryArrayKey(arrayKeys)
	cols, rows := recordsTable(obj[primary].([]any))
	view.cols, view.rows = cols, rows
	for _, k := range arrayKeys {
		if k == primary {
			continue
		}
		view.notes = append(view.notes, fmt.Sprintf("%s: [%d items]", k, len(obj[k].([]any))))
	}
	return view
}

// primaryArrayKey chooses which array becomes the main table when a response
// carries several. It prefers a conventional data key ("rows", "items",
// "data", "results") over the alphabetically-first one — otherwise a shape like
// {columnHeaders:[…], rows:[…]} (GA4 and friends) would table the column
// metadata and demote the actual rows to a note. keys is assumed sorted.
func primaryArrayKey(keys []string) string {
	for _, want := range []string{"rows", "items", "data", "results"} {
		for _, k := range keys {
			if k == want {
				return k
			}
		}
	}
	return keys[0]
}

// recordsTable turns an array into table columns + rows. An array of objects
// yields a column per union key (sorted); an array of scalars yields a single
// "value" column.
func recordsTable(items []any) (cols []string, rows [][]string) {
	colSet := map[string]bool{}
	anyObject := false
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			anyObject = true
			for k := range m {
				colSet[k] = true
			}
		}
	}
	if !anyObject {
		rows = make([][]string, 0, len(items))
		for _, it := range items {
			rows = append(rows, []string{scalarString(it)})
		}
		return []string{"value"}, rows
	}
	for k := range colSet {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	rows = make([][]string, 0, len(items))
	for _, it := range items {
		m, _ := it.(map[string]any)
		row := make([]string, len(cols))
		for i, c := range cols {
			if m != nil {
				row[i] = scalarString(m[c])
			}
		}
		rows = append(rows, row)
	}
	return cols, rows
}

// scalarString renders a single JSON value as a flat cell. Objects and arrays
// become compact JSON so the table stays one row per record.
func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

// renderTabular writes the lead lines + table for v. markdown=true emits a
// GitHub-flavored Markdown table; otherwise an aligned ASCII table.
func renderTabular(w io.Writer, v any, markdown bool) error {
	view := buildView(v)
	if len(view.leadKeys) > 0 {
		for _, k := range view.leadKeys {
			if _, err := fmt.Fprintf(w, "%s: %s\n", k, view.lead[k]); err != nil {
				return err
			}
		}
		if len(view.rows) > 0 {
			_, _ = fmt.Fprintln(w)
		}
	}
	switch {
	case len(view.rows) > 0 && len(view.cols) > 0:
		var err error
		if markdown {
			err = writeMarkdownTable(w, view.cols, view.rows)
		} else {
			err = writeASCIITable(w, view.cols, view.rows)
		}
		if err != nil {
			return err
		}
	case len(view.leadKeys) == 0 || len(view.cols) > 0:
		// Nothing to tabulate: either a truly empty result, or an object with
		// scalar siblings whose primary array came back empty. Either way say
		// so explicitly rather than printing nothing (silent empty result).
		if _, err := fmt.Fprintln(w, "(no rows)"); err != nil {
			return err
		}
	}
	for _, n := range view.notes {
		if _, err := fmt.Fprintln(w, n); err != nil {
			return err
		}
	}
	return nil
}

// writeASCIITable prints a left-aligned, space-padded table with a dashed
// underline beneath the header.
func writeASCIITable(w io.Writer, cols []string, rows [][]string) error {
	widths := columnWidths(cols, rows)
	var b strings.Builder
	writeRow := func(cells []string) {
		for i, c := range cells {
			cell := truncateCell(c)
			b.WriteString(cell)
			if i < len(cells)-1 && i < len(widths) {
				b.WriteString(strings.Repeat(" ", widths[i]-stringDisplayWidth(cell)+2))
			}
		}
		b.WriteByte('\n')
	}
	writeRow(cols)
	seps := make([]string, len(cols))
	for i := range seps {
		seps[i] = strings.Repeat("-", widths[i])
	}
	writeRow(seps)
	for _, r := range rows {
		writeRow(r)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeMarkdownTable prints a GitHub-flavored Markdown table.
func writeMarkdownTable(w io.Writer, cols []string, rows [][]string) error {
	var b strings.Builder
	esc := func(s string) string {
		s = strings.ReplaceAll(s, "\r", " ")
		s = strings.ReplaceAll(s, "\n", " ")
		return strings.ReplaceAll(s, "|", "\\|")
	}
	writeRow := func(cells []string) {
		b.WriteString("| ")
		for i, c := range cells {
			b.WriteString(esc(truncateCell(c)))
			if i < len(cells)-1 {
				b.WriteString(" | ")
			}
		}
		b.WriteString(" |\n")
	}
	writeRow(cols)
	seps := make([]string, len(cols))
	for i := range seps {
		seps[i] = "---"
	}
	writeRow(seps)
	for _, r := range rows {
		writeRow(r)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// columnWidths returns the rendered width of each column (header vs widest
// truncated cell).
func columnWidths(cols []string, rows [][]string) []int {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = stringDisplayWidth(c)
	}
	for _, r := range rows {
		for i, c := range r {
			if i >= len(widths) {
				continue
			}
			if n := stringDisplayWidth(truncateCell(c)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

// truncateCell caps a cell at maxCellWidth terminal columns with a trailing
// ellipsis (U+2026, 1 column). Wide runes (2 columns) are never split: if
// adding the next rune would exceed the budget the loop stops early, which also
// keeps the writeASCIITable padding count from going negative at a boundary.
func truncateCell(s string) string {
	if stringDisplayWidth(s) <= maxCellWidth {
		return s
	}
	const budget = maxCellWidth - 1 // leave 1 column for the ellipsis
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := displayWidth(r)
		if used+w > budget {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// renderCSV writes the primary record set as CSV (RFC 4180 via encoding/csv).
// A flat object becomes field,value rows. Cells are passed through
// neutralizeCSVCell so attacker-controlled API content (a Drive filename, an
// email subject, a URL) can't smuggle a spreadsheet formula into Excel/Sheets.
func renderCSV(w io.Writer, v any) error {
	view := buildView(v)
	cw := csv.NewWriter(w)
	if len(view.cols) > 0 {
		if err := cw.Write(neutralizeRow(view.cols)); err != nil {
			return err
		}
		for _, r := range view.rows {
			if err := cw.Write(neutralizeRow(r)); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

func neutralizeRow(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = neutralizeCSVCell(c)
	}
	return out
}

// neutralizeCSVCell defuses CSV/spreadsheet formula injection: a cell beginning
// with =, +, -, @, or a control char is prefixed with a single quote so a
// spreadsheet treats it as text rather than evaluating it. Leading whitespace is
// considered: LibreOffice Calc trims leading ASCII whitespace before deciding whether a cell is a
// formula, so " =SUM()" must be defused too — the quote is prefixed to the
// ORIGINAL string so the displayed value is unchanged for non-formula cells.
func neutralizeCSVCell(s string) string {
	if s == "" {
		return s
	}
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if trimmed == "" {
		return s
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + s
	}
	return s
}
