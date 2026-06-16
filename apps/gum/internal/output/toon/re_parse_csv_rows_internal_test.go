package toon

import (
	"strings"
	"testing"
)

// TestReParseCSVRowsBranches pins each early-return arm of
// reParseCSVRows. A regression that returns nil instead of a parsed
// row (or vice versa) would silently change the typed-decode shape
// seen by clients of DecodeTOONDocumentTyped.
func TestReParseCSVRowsBranches(t *testing.T) {
	t.Run("missing_blank_line_separator_errors", func(t *testing.T) {
		// No double-newline → can't find the body.
		rows, err := reParseCSVRows([]byte("header only\n"), 0)
		if err == nil {
			t.Fatal("want error; got nil")
		}
		if !strings.Contains(err.Error(), "missing blank-line separator") {
			t.Errorf("err=%v; want 'missing blank-line separator'", err)
		}
		if rows != nil {
			t.Errorf("rows=%v; want nil on error", rows)
		}
	})

	t.Run("empty_object_sentinel_returns_nil", func(t *testing.T) {
		// The "count=0" sentinel `{}` body returns (nil, nil).
		rows, err := reParseCSVRows([]byte("header\n\n{}\n"), 0)
		if err != nil {
			t.Fatalf("err=%v; want nil for {} sentinel", err)
		}
		if rows != nil {
			t.Errorf("rows=%v; want nil for {} sentinel", rows)
		}
	})

	t.Run("empty_body_after_separator_returns_nil", func(t *testing.T) {
		// Header + blank line + nothing after the separator → (nil, nil).
		rows, err := reParseCSVRows([]byte("header\n\n"), 0)
		if err != nil {
			t.Fatalf("err=%v; want nil for empty body", err)
		}
		if rows != nil {
			t.Errorf("rows=%v; want nil for empty body", rows)
		}
	})

	t.Run("single_row_parses", func(t *testing.T) {
		rows, err := reParseCSVRows([]byte("header\n\nfoo,bar\n"), 0)
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("len(rows)=%d; want 1", len(rows))
		}
		if len(rows[0]) != 2 {
			t.Errorf("len(rows[0])=%d; want 2", len(rows[0]))
		}
		if rows[0][0].value != "foo" || rows[0][1].value != "bar" {
			t.Errorf("rows[0]=%+v; want [foo, bar]", rows[0])
		}
	})
}
