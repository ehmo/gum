// lossless_test.go — stage 8 is an encoder, not a filter (spec §9.1, docs/spec.md:2050).
//
// Two ways the renderer used to delete data it was handed: CSV wrote only the
// primary record table, so a top-level nextPageToken and every secondary array
// vanished with no notice anywhere in the output; and the markdown encoder cut
// every cell at 60 terminal columns, which is a terminal-layout rule applied to
// a document format, overriding whatever limit the profile's truncate_strings
// stage had already agreed.
package render

import (
	"fmt"
	"strings"
	"testing"
)

// TestRenderCSVKeepsTopLevelScalars pins the page token. An agent that asked
// for csv and got rows back with no token cannot fetch page 2.
func TestRenderCSVKeepsTopLevelScalars(t *testing.T) {
	out := renderToString(t, "csv", `{"nextPageToken":"tok-2","rows":[{"clicks":5},{"clicks":7}]}`)
	want := "clicks,nextPageToken\n5,tok-2\n7,tok-2\n"
	if out != want {
		t.Errorf("csv = %q, want %q", out, want)
	}
}

// TestRenderCSVKeepsSecondaryArrayCounts pins the other half: the arrays the
// primary table did not take are summarised in table/markdown output and were
// dropped outright from csv.
func TestRenderCSVKeepsSecondaryArrayCounts(t *testing.T) {
	out := renderToString(t, "csv", `{"rows":[{"a":1}],"columnHeaders":[{"name":"q"},{"name":"p"}]}`)
	want := "a,columnHeaders\n1,[2 items]\n"
	if out != want {
		t.Errorf("csv = %q, want %q", out, want)
	}
}

// TestRenderCSVEnvelopeSurvivesZeroRows covers the empty page that still
// carries a token. There is no record schema to preserve, so the envelope is
// the whole table.
func TestRenderCSVEnvelopeSurvivesZeroRows(t *testing.T) {
	out := renderToString(t, "csv", `{"nextPageToken":"tok-2","rows":[]}`)
	want := "nextPageToken\ntok-2\n"
	if out != want {
		t.Errorf("csv = %q, want %q", out, want)
	}
}

// TestRenderCSVEnvelopeNameCollision pins the disambiguation: a top-level key
// with the same name as a record column keeps both values under distinct
// headers rather than one silently winning.
func TestRenderCSVEnvelopeNameCollision(t *testing.T) {
	out := renderToString(t, "csv", `{"status":"OK","rows":[{"status":"row-status"}]}`)
	want := "status,_status\nrow-status,OK\n"
	if out != want {
		t.Errorf("csv = %q, want %q", out, want)
	}
}

// TestRenderCSVUnchangedWithoutAnEnvelope guards the common case: a plain
// object-wrapping-one-array response gains no columns.
func TestRenderCSVUnchangedWithoutAnEnvelope(t *testing.T) {
	out := renderToString(t, "csv", `{"rows":[{"a":1,"b":2}]}`)
	want := "a,b\n1,2\n"
	if out != want {
		t.Errorf("csv = %q, want %q", out, want)
	}
}

// TestRenderMarkdownDoesNotTruncate pins the encoder rule. String length is
// stage 6's job (truncate_strings); stage 8 encodes what stage 6 handed it.
func TestRenderMarkdownDoesNotTruncate(t *testing.T) {
	long := strings.Repeat("a", 200)
	out := renderToString(t, "markdown", fmt.Sprintf(`[{"v":%q}]`, long))
	if !strings.Contains(out, long) {
		t.Errorf("markdown cut a %d-char cell:\n%s", len(long), out)
	}
	if strings.Contains(out, "…") {
		t.Errorf("markdown wrote a truncation ellipsis:\n%s", out)
	}
}

// TestRenderCSVDoesNotTruncate guards the same rule for csv, which never
// truncated and must not start.
func TestRenderCSVDoesNotTruncate(t *testing.T) {
	long := strings.Repeat("a", 200)
	out := renderToString(t, "csv", fmt.Sprintf(`[{"v":%q}]`, long))
	if !strings.Contains(out, long) {
		t.Errorf("csv cut a %d-char cell:\n%s", len(long), out)
	}
}

// TestRenderTableStillTruncates is the boundary of the fix. The ASCII table is
// a terminal layout, so its 60-column cap stays.
func TestRenderTableStillTruncates(t *testing.T) {
	long := strings.Repeat("a", 200)
	out := renderToString(t, "table", fmt.Sprintf(`[{"v":%q}]`, long))
	if strings.Contains(out, long) {
		t.Errorf("ascii table stopped capping cell width:\n%s", out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("ascii table dropped the truncation ellipsis:\n%s", out)
	}
}
