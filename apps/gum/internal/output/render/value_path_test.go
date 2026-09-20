package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestValuePathRecognition pins the selector grammar. The prefix match is
// case-insensitive so `--output VALUE(x)` works, but the path inside is handed
// back verbatim: JSON field names are case-sensitive.
func TestValuePathRecognition(t *testing.T) {
	cases := []struct {
		format  string
		want    string
		wantOK  bool
		comment string
	}{
		{format: "value(rows[0].clicks)", want: "rows[0].clicks", wantOK: true},
		{format: "VALUE(SiteUrl)", want: "SiteUrl", wantOK: true, comment: "path keeps its case"},
		{format: "value()", want: "", wantOK: true, comment: "empty path selects the whole value"},
		{format: "value(rows", wantOK: false, comment: "no closing paren"},
		{format: "table", wantOK: false},
		{format: "", wantOK: false},
	}
	for _, c := range cases {
		got, ok := ValuePath(c.format)
		if ok != c.wantOK || got != c.want {
			t.Errorf("ValuePath(%q)=(%q,%v); want (%q,%v) %s", c.format, got, ok, c.want, c.wantOK, c.comment)
		}
	}
}

// TestRenderValueSelector covers the gcloud-style scripting format end to end:
// one scalar per line, no headers, and a silent empty result for any path that
// does not match.
func TestRenderValueSelector(t *testing.T) {
	const body = `{"responseAggregationType":"byProperty","rows":[{"clicks":5,"page":"/a"},{"clicks":7,"page":"/b"}],"nextPageToken":"tok"}`
	cases := []struct {
		name   string
		format string
		body   string
		want   string
	}{
		{name: "indexed", format: "value(rows[0].clicks)", body: body, want: "5\n"},
		{name: "fan_out", format: "value(rows[].page)", body: body, want: "/a\n/b\n"},
		{name: "whole_array", format: "value(rows[])", body: `{"rows":["a","b"]}`, want: "a\nb\n"},
		{name: "top_scalar", format: "value(nextPageToken)", body: body, want: "tok\n"},
		{name: "empty_path", format: "value()", body: `"just-a-string"`, want: "just-a-string\n"},
		{name: "missing_key", format: "value(nope.deeper)", body: body, want: ""},
		{name: "index_out_of_range", format: "value(rows[9].clicks)", body: body, want: ""},
		{name: "negative_index", format: "value(rows[-1])", body: body, want: ""},
		{name: "non_numeric_index", format: "value(rows[x])", body: body, want: ""},
		{name: "key_on_scalar", format: "value(nextPageToken.inner)", body: body, want: ""},
		{name: "index_on_object", format: "value(rows[0][0])", body: body, want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderToString(t, c.format, c.body); got != c.want {
				t.Errorf("Structured(%s)=%q; want %q", c.format, got, c.want)
			}
		})
	}
}

// TestScalarStringVariants pins the cell rendering for every JSON type. A nil
// must render empty, not "<nil>", and a whole float must lose its ".0" or every
// count in every table reads as a float.
func TestScalarStringVariants(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{name: "nil", in: nil, want: ""},
		{name: "string", in: "plain", want: "plain"},
		{name: "bool", in: true, want: "true"},
		{name: "whole_float", in: float64(42), want: "42"},
		{name: "fractional_float", in: 1.5, want: "1.5"},
		{name: "json_number", in: json.Number("1e400"), want: "1e400"},
		{name: "object", in: map[string]any{"k": "v"}, want: `{"k":"v"}`},
		{name: "array", in: []any{1.0, "x"}, want: `[1,"x"]`},
		{name: "unmarshalable", in: make(chan int), want: "<nil>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scalarString(c.in)
			if c.name == "unmarshalable" {
				// json.Marshal fails on a channel; the fallback prints the Go
				// value rather than dropping the cell.
				if got == "" {
					t.Fatal("scalarString(chan) returned an empty cell; want the Go-value fallback")
				}
				return
			}
			if got != c.want {
				t.Errorf("scalarString(%v)=%q; want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSecondaryArraysReportedAsNotes proves the arrays the primary table did
// not take are still named in the output. Dropping them silently would hide a
// whole result section.
func TestSecondaryArraysReportedAsNotes(t *testing.T) {
	const body = `{"rows":[{"a":1}],"columnHeaders":[{"name":"a"},{"name":"b"}],"warnings":["w"]}`

	table := renderToString(t, "table", body)
	if !strings.Contains(table, "columnHeaders: [2 items]") {
		t.Errorf("table output %q lost the columnHeaders note", table)
	}
	if !strings.Contains(table, "warnings: [1 items]") {
		t.Errorf("table output %q lost the warnings note", table)
	}

	csvOut := renderToString(t, "csv", body)
	if !strings.Contains(csvOut, "columnHeaders") || !strings.Contains(csvOut, "[2 items]") {
		t.Errorf("csv output %q lost the columnHeaders column", csvOut)
	}
}

// TestCSVEnvelopeOnlyWhenNoRecords covers the empty-page shape: no records, but
// a page token the caller needs to fetch the next page.
func TestCSVEnvelopeOnlyWhenNoRecords(t *testing.T) {
	got := renderToString(t, "csv", `{"nextPageToken":"tok","rows":[]}`)
	if !strings.Contains(got, "nextPageToken") || !strings.Contains(got, "tok") {
		t.Errorf("csv output %q dropped the page token", got)
	}
}

// errWriter fails every write so the renderers' error returns are exercised
// rather than assumed. A dropped write error would truncate output silently.
type errWriter struct{ err error }

func (e errWriter) Write([]byte) (int, error) { return 0, e.err }

// TestStructuredPropagatesWriteErrors proves no format swallows a write
// failure. A closed pipe on stdout must reach the caller's exit code.
func TestStructuredPropagatesWriteErrors(t *testing.T) {
	boom := errors.New("disk full")
	bodies := map[string]string{
		"table":                `{"rows":[{"a":1}],"extra":["x"]}`,
		"markdown":             `{"rows":[{"a":1}],"extra":["x"]}`,
		"csv":                  `{"rows":[{"a":1}]}`,
		"csv_envelope_only":    `{"nextPageToken":"tok"}`,
		"csv_cols_only":        `{"rows":[]}`,
		"value(rows[0].a)":     `{"rows":[{"a":1}]}`,
		"json":                 `{"a":1}`,
		"table_lead_only":      `{"scalar":"s","rows":[]}`,
		"table_scalar_no_rows": `{}`,
	}
	formats := map[string]string{
		"table":                "table",
		"markdown":             "markdown",
		"csv":                  "csv",
		"csv_envelope_only":    "csv",
		"csv_cols_only":        "csv",
		"value(rows[0].a)":     "value(rows[0].a)",
		"json":                 "unrecognised-format",
		"table_lead_only":      "table",
		"table_scalar_no_rows": "table",
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			err := Structured(errWriter{err: boom}, formats[name], parseJSON(t, body))
			if !errors.Is(err, boom) {
				t.Errorf("Structured(%s) err=%v; want %v", formats[name], err, boom)
			}
		})
	}
}

// TestWideCellNeverSplitsARune covers the truncation boundary: a cell of wide
// runes must stop short of the budget rather than emit half a rune and throw
// the ASCII padding negative.
func TestWideCellNeverSplitsARune(t *testing.T) {
	wide := strings.Repeat("世", maxCellWidth)
	got := truncateCell(wide)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncateCell(%d wide runes)=%q; want a trailing ellipsis", maxCellWidth, got)
	}
	if w := stringDisplayWidth(got); w > maxCellWidth {
		t.Errorf("truncated width=%d; want <= %d", w, maxCellWidth)
	}
	var b bytes.Buffer
	if err := writeASCIITable(&b, []string{"c"}, [][]string{{wide}}); err != nil {
		t.Fatalf("writeASCIITable: %v", err)
	}
}

// TestBareScalarRendersAsValueColumn covers the shape a result takes when the
// whole body is one scalar: a single "value" column, not an empty table.
func TestBareScalarRendersAsValueColumn(t *testing.T) {
	got := renderToString(t, "table", `42`)
	if !strings.Contains(got, "value") || !strings.Contains(got, "42") {
		t.Errorf("table output %q; want a value column holding 42", got)
	}
}

// TestColumnWidthsIgnoresExtraCells covers the ragged-row guard: a row wider
// than the header must not index past the width slice.
func TestColumnWidthsIgnoresExtraCells(t *testing.T) {
	got := columnWidths([]string{"a"}, [][]string{{"xx", "ignored-because-wider"}})
	if len(got) != 1 {
		t.Fatalf("columnWidths len=%d; want 1", len(got))
	}
	if got[0] != 2 {
		t.Errorf("width=%d; want 2 from the only in-range cell", got[0])
	}
}
