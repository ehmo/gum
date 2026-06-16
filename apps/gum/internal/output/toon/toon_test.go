// Package toon_test contains normative §9.0 conformance tests for the TOON
// canonical format as specified in docs/spec.md §9.0.
//
// These tests target the §9.0 two-section document format:
//
//	Header (YAML-style key: value lines)
//	<blank line>
//	Body (RFC 4180 CSV rows, field order from "fields" header)
//
// Required exported symbols (green team must add if absent):
//   - ErrToonVersionUnsupported — sentinel error for format_version != 1
//   - DecodeTOONDocument([]byte) (*TOONDocument, error) — parse §9.0 header+body
//   - EncodeTOONDocument(doc TOONDocument) ([]byte, error) — emit §9.0 header+body
//
// TOONDocument must expose at minimum:
//
//	Op             string
//	Variant        string
//	Count          int
//	Fields         []string
//	FormatVersion  int
//	NextPageToken  string  // optional; empty means absent
//	Rows           [][]any // decoded body rows; nil/empty when Count==0
package toon_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildTOONDoc constructs a minimal §9.0 TOON document string from parts.
func buildTOONDoc(headers map[string]string, body string) string {
	// Emit headers in a deterministic order: op, variant, format_version, count, fields, extras.
	order := []string{"op", "variant", "format_version", "count", "fields"}
	var sb strings.Builder
	for _, k := range order {
		if v, ok := headers[k]; ok {
			sb.WriteString(k)
			sb.WriteString(": ")
			sb.WriteString(v)
			sb.WriteByte('\n')
		}
	}
	// Any remaining keys (e.g. next_page_token).
	for k, v := range headers {
		skip := false
		for _, o := range order {
			if o == k {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(v)
		sb.WriteByte('\n')
	}
	sb.WriteByte('\n') // blank line separating header from body
	sb.WriteString(body)
	return sb.String()
}

// ---------------------------------------------------------------------------
// TestToonDocumentRoundTrip — §9.0 normative round-trip using the two-section
// header+body TOON document format.
//
// Spec ref: docs/spec.md line 1828
// "TestToonRoundTrip in internal/output/toon_test.go MUST verify that for each
// representative TOON fixture in testdata/toon/ ... the GUM TOON parser
// round-trips the document to the same typed Go values as the original upstream
// fixture."
//
// The four fixtures correspond to the spec's enumerated minimum set:
//   - list-with-nulls   — list result with null fields
//   - quoted-csv        — result with quoted CSV fields
//   - zero-omitted-count — result with zero omitted_count
//   - empty-body        — empty-body result
//
// NOTE: This test uses DecodeTOONDocument / EncodeTOONDocument — the §9.0
// two-section parser — NOT the generic Encode/Decode. It will fail until the
// green team adds those symbols.
// ---------------------------------------------------------------------------
func TestToonDocumentRoundTrip(t *testing.T) {

	// Fixture: list-with-nulls
	// doc carries op, variant, format_version, count, fields header + CSV body.
	// After decode→encode→decode the typed rows must match.
	listNullsDoc := buildTOONDoc(map[string]string{
		"op":             "gmail.users.messages.list",
		"variant":        "gmail.v1.rest.users.messages.list",
		"format_version": "1",
		"count":          "2",
		"fields":         "id,snippet,subject",
	}, "msg001,,Hello\nmsg002,,\"Re: Hello\"\n")

	roundTrip(t, "list-with-nulls", listNullsDoc)

	// Fixture: quoted-csv — strings with comma, double-quote, newline
	quotedDoc := buildTOONDoc(map[string]string{
		"op":             "gmail.users.messages.list",
		"variant":        "gmail.v1.rest.users.messages.list",
		"format_version": "1",
		"count":          "2",
		"fields":         "id,snippet,subject",
	}, "m1,\"He said \"\"hi\"\"\",\"Hello, World\"\nm2,normal,\"Line\nBreak\"\n")

	roundTrip(t, "quoted-csv", quotedDoc)

	// Fixture: zero-omitted-count — omitted_count is 0
	zeroOmittedDoc := buildTOONDoc(map[string]string{
		"op":             "gmail.users.labels.list",
		"variant":        "gmail.v1.rest.users.labels.list",
		"format_version": "1",
		"count":          "1",
		"fields":         "name,total",
	}, "inbox,42\n")

	roundTrip(t, "zero-omitted-count", zeroOmittedDoc)

	// Fixture: empty-body — count=0 → body is exactly "{}"
	emptyBodyDoc := buildTOONDoc(map[string]string{
		"op":             "gmail.users.messages.list",
		"variant":        "gmail.v1.rest.users.messages.list",
		"format_version": "1",
		"count":          "0",
		"fields":         "id,snippet",
	}, "{}")

	roundTrip(t, "empty-body", emptyBodyDoc)
}

// roundTrip is a helper: decode→encode→decode must produce identical typed rows.
func roundTrip(t *testing.T, name, raw string) {
	t.Helper()
	doc1, err := toon.DecodeTOONDocument([]byte(raw))
	if err != nil {
		t.Fatalf("[%s] DecodeTOONDocument: %v", name, err)
	}
	reencoded, err := toon.EncodeTOONDocument(*doc1)
	if err != nil {
		t.Fatalf("[%s] EncodeTOONDocument: %v", name, err)
	}
	doc2, err := toon.DecodeTOONDocument(reencoded)
	if err != nil {
		t.Fatalf("[%s] DecodeTOONDocument (round-trip): %v", name, err)
	}
	if doc1.Op != doc2.Op || doc1.Variant != doc2.Variant || doc1.Count != doc2.Count {
		t.Errorf("[%s] header mismatch after round-trip: got %+v want %+v", name, doc2, doc1)
	}
	if len(doc1.Rows) != len(doc2.Rows) {
		t.Errorf("[%s] row count mismatch: got %d want %d", name, len(doc2.Rows), len(doc1.Rows))
		return
	}
	for i, r1 := range doc1.Rows {
		r2 := doc2.Rows[i]
		if len(r1) != len(r2) {
			t.Errorf("[%s] row %d length mismatch: got %d want %d", name, i, len(r2), len(r1))
			continue
		}
		for j, v1 := range r1 {
			v2 := r2[j]
			if v1 != v2 {
				t.Errorf("[%s] row %d col %d: got %v (%T) want %v (%T)", name, i, j, v2, v2, v1, v1)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TestToonVersionUnsupported — §9.0 forward-compatibility (normative).
//
// Spec ref: docs/spec.md line 1832
// "A TOON parser that reads a document carrying format_version greater than its
// highest-supported version … MUST fail closed with the structured error
// TOON_VERSION_UNSUPPORTED."
// "format_version values less than 1 are also rejected with the same code."
// ---------------------------------------------------------------------------
func TestToonVersionUnsupported(t *testing.T) {
	cases := []struct {
		name    string
		version string
	}{
		{"version_2_unsupported", "2"},
		{"version_0_rejected", "0"},
		{"version_negative_rejected", "-1"},
		{"version_999_rejected", "999"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := buildTOONDoc(map[string]string{
				"op":             "any.op",
				"variant":        "any.variant",
				"format_version": tc.version,
				"count":          "0",
				"fields":         "id",
			}, "{}")

			_, err := toon.DecodeTOONDocument([]byte(doc))
			if err == nil {
				t.Fatalf("DecodeTOONDocument with format_version=%s: want error, got nil", tc.version)
			}
			// Must be (or wrap) ErrToonVersionUnsupported.
			if !errors.Is(err, toon.ErrToonVersionUnsupported) {
				t.Errorf("error %v does not satisfy errors.Is(err, ErrToonVersionUnsupported)", err)
			}
			// Error message must contain TOON_VERSION_UNSUPPORTED per spec §9.0.
			if !strings.Contains(err.Error(), "TOON_VERSION_UNSUPPORTED") {
				t.Errorf("error message %q missing TOON_VERSION_UNSUPPORTED", err.Error())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestToonNullRepresentation — §9.0 null-sentinel normative rule.
//
// Spec ref: docs/spec.md line 1817–1823
// "Null value: empty field (two consecutive commas, or a trailing comma)."
// "Empty string (""):  a double-quoted empty field, \"\"."
// ---------------------------------------------------------------------------
func TestToonNullRepresentation(t *testing.T) {
	// Spec example: third row has trailing comma → snippet is null.
	// row: id,snippet  → "abc," means snippet is null.
	docWithNulls := buildTOONDoc(map[string]string{
		"op":             "test.op",
		"variant":        "test.variant",
		"format_version": "1",
		"count":          "3",
		"fields":         "id,a,b",
	}, // null in middle: two consecutive commas
	// null at end: trailing comma
	// empty string: \"\"
	"row1,,end\nrow2,mid,\nrow3,\"\",last\n")

	doc, err := toon.DecodeTOONDocument([]byte(docWithNulls))
	if err != nil {
		t.Fatalf("DecodeTOONDocument: %v", err)
	}
	if len(doc.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(doc.Rows))
	}
	// row1: a is null (consecutive commas)
	if doc.Rows[0][1] != nil {
		t.Errorf("row1.a: got %v (%T), want nil (null via consecutive commas)", doc.Rows[0][1], doc.Rows[0][1])
	}
	// row2: b is null (trailing comma)
	if doc.Rows[1][2] != nil {
		t.Errorf("row2.b: got %v (%T), want nil (null via trailing comma)", doc.Rows[1][2], doc.Rows[1][2])
	}
	// row3: a is empty string (quoted "")
	if doc.Rows[2][1] != "" {
		t.Errorf("row3.a: got %v (%T), want \"\" (empty string via quoted empty field)", doc.Rows[2][1], doc.Rows[2][1])
	}

	// Encoding: null → empty CSV cell, empty string → ""
	t.Run("encode_null_to_empty_cell", func(t *testing.T) {
		encDoc := toon.TOONDocument{
			Op:            "test.op",
			Variant:       "test.variant",
			Count:         2,
			Fields:        []string{"id", "name", "extra"},
			FormatVersion: 1,
			Rows: [][]any{
				{"r1", nil, "ok"},    // nil in middle → consecutive commas
				{"r2", "val", nil},   // nil at end → trailing comma
			},
		}
		encoded, err := toon.EncodeTOONDocument(encDoc)
		if err != nil {
			t.Fatalf("EncodeTOONDocument: %v", err)
		}
		body := extractBody(t, string(encoded))
		lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
		if len(lines) < 2 {
			t.Fatalf("expected at least 2 data rows, got %d", len(lines))
		}
		// Row 0: null in middle must be two consecutive commas (,,)
		if !strings.Contains(lines[0], ",,") {
			t.Errorf("row0: expected consecutive commas for null in middle, got %q", lines[0])
		}
		// Row 1: null at end must be trailing comma
		if !strings.HasSuffix(lines[1], ",") {
			t.Errorf("row1: expected trailing comma for null at end, got %q", lines[1])
		}
	})
}

// extractBody returns the body section (after the blank-line separator) of a §9.0 doc.
func extractBody(t *testing.T, doc string) string {
	t.Helper()
	idx := strings.Index(doc, "\n\n")
	if idx < 0 {
		t.Fatalf("no blank-line separator found in doc:\n%s", doc)
	}
	return doc[idx+2:]
}

// ---------------------------------------------------------------------------
// TestToonQuotedCSV — §9.0 RFC 4180 quoting rules (normative).
//
// Spec ref: docs/spec.md line 1802
// "String fields containing commas, double quotes, or newlines are
// double-quote-quoted with internal \" escaped as \"\"."
// ---------------------------------------------------------------------------
func TestToonQuotedCSV(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantSub string // substring that must appear in the CSV cell
	}{
		{
			name:    "comma_triggers_quoting",
			value:   "hello, world",
			wantSub: `"hello, world"`,
		},
		{
			name:    "double_quote_escaped_as_doubled",
			value:   `say "hi"`,
			wantSub: `"say ""hi"""`,
		},
		{
			name:    "newline_triggers_quoting",
			value:   "line1\nline2",
			wantSub: "\"line1\nline2\"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := toon.TOONDocument{
				Op:            "test.op",
				Variant:       "test.variant",
				Count:         1,
				Fields:        []string{"id", "val"},
				FormatVersion: 1,
				Rows:          [][]any{{"r1", tc.value}},
			}
			encoded, err := toon.EncodeTOONDocument(doc)
			if err != nil {
				t.Fatalf("EncodeTOONDocument: %v", err)
			}
			body := extractBody(t, string(encoded))
			if !strings.Contains(body, tc.wantSub) {
				t.Errorf("encoded body does not contain %q\nbody:\n%s", tc.wantSub, body)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestToonEmptyBodySentinel — §9.0 empty-body sentinel (normative).
//
// Spec ref: docs/spec.md line 1828
// "an empty-body result" is one of the four normative fixtures.
// Spec line 1801 count=0 implies no data rows; the body section emits "{}".
//
// Cross-reference: encoder.go comment line 12:
// "An object with all-empty fields encodes as "{}" (empty object sentinel)"
// For §9.0 documents, count=0 → body MUST be exactly "{}".
// ---------------------------------------------------------------------------
func TestToonEmptyBodySentinel(t *testing.T) {
	doc := toon.TOONDocument{
		Op:            "test.op",
		Variant:       "test.variant",
		Count:         0,
		Fields:        []string{"id", "name"},
		FormatVersion: 1,
		Rows:          nil,
	}
	encoded, err := toon.EncodeTOONDocument(doc)
	if err != nil {
		t.Fatalf("EncodeTOONDocument: %v", err)
	}
	body := strings.TrimRight(extractBody(t, string(encoded)), "\n")
	if body != "{}" {
		t.Errorf("count=0 body: got %q, want \"{}\"", body)
	}
}

// ---------------------------------------------------------------------------
// TestToonHeaderKeysRequired — §9.0 normative required header keys.
//
// Spec ref: docs/spec.md line 1801
// "required keys are op, variant, count, fields, and format_version: 1"
// Missing any required key MUST be an error.
// ---------------------------------------------------------------------------
func TestToonHeaderKeysRequired(t *testing.T) {
	fullHeaders := map[string]string{
		"op":             "test.op",
		"variant":        "test.variant",
		"format_version": "1",
		"count":          "0",
		"fields":         "id",
	}

	requiredKeys := []string{"op", "variant", "format_version", "count", "fields"}

	for _, missing := range requiredKeys {
		t.Run("missing_"+missing, func(t *testing.T) {
			headers := make(map[string]string, len(fullHeaders)-1)
			for k, v := range fullHeaders {
				if k != missing {
					headers[k] = v
				}
			}
			doc := buildTOONDoc(headers, "{}")
			_, err := toon.DecodeTOONDocument([]byte(doc))
			if err == nil {
				t.Errorf("DecodeTOONDocument with missing header %q: want error, got nil", missing)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestToonNextPageTokenOptional — §9.0 optional next_page_token header.
//
// Spec ref: docs/spec.md line 1801
// "Optional keys: next_page_token (omitted if no next page)."
// ---------------------------------------------------------------------------
func TestToonNextPageTokenOptional(t *testing.T) {
	t.Run("without_next_page_token", func(t *testing.T) {
		doc := buildTOONDoc(map[string]string{
			"op":             "test.op",
			"variant":        "test.variant",
			"format_version": "1",
			"count":          "0",
			"fields":         "id",
		}, "{}")
		result, err := toon.DecodeTOONDocument([]byte(doc))
		if err != nil {
			t.Fatalf("DecodeTOONDocument without next_page_token: %v", err)
		}
		if result.NextPageToken != "" {
			t.Errorf("NextPageToken: got %q, want empty (absent)", result.NextPageToken)
		}
	})

	t.Run("with_next_page_token", func(t *testing.T) {
		doc := buildTOONDoc(map[string]string{
			"op":              "test.op",
			"variant":         "test.variant",
			"format_version":  "1",
			"count":           "0",
			"fields":          "id",
			"next_page_token": "tok_abc123",
		}, "{}")
		result, err := toon.DecodeTOONDocument([]byte(doc))
		if err != nil {
			t.Fatalf("DecodeTOONDocument with next_page_token: %v", err)
		}
		if result.NextPageToken != "tok_abc123" {
			t.Errorf("NextPageToken: got %q, want %q", result.NextPageToken, "tok_abc123")
		}
	})

	// When encoded, next_page_token must appear in the header iff non-empty.
	t.Run("encode_omits_empty_next_page_token", func(t *testing.T) {
		d := toon.TOONDocument{
			Op:            "test.op",
			Variant:       "test.variant",
			Count:         0,
			Fields:        []string{"id"},
			FormatVersion: 1,
			Rows:          nil,
		}
		enc, err := toon.EncodeTOONDocument(d)
		if err != nil {
			t.Fatalf("EncodeTOONDocument: %v", err)
		}
		if strings.Contains(string(enc), "next_page_token") {
			t.Errorf("encoded doc contains next_page_token but field was empty:\n%s", enc)
		}
	})

	t.Run("encode_includes_next_page_token_when_set", func(t *testing.T) {
		d := toon.TOONDocument{
			Op:            "test.op",
			Variant:       "test.variant",
			Count:         0,
			Fields:        []string{"id"},
			FormatVersion: 1,
			NextPageToken: "tok_xyz",
			Rows:          nil,
		}
		enc, err := toon.EncodeTOONDocument(d)
		if err != nil {
			t.Fatalf("EncodeTOONDocument: %v", err)
		}
		if !strings.Contains(string(enc), "next_page_token: tok_xyz") {
			t.Errorf("encoded doc missing next_page_token:\n%s", enc)
		}
	})
}
