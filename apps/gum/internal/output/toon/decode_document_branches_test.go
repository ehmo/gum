package toon_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestDecodeMissingBlankLineSeparator pins DecodeTOONDocument's
// `sepIdx < 0 → error` arm (document.go:48-50). A header-only payload
// without the mandatory blank-line separator is malformed.
func TestDecodeMissingBlankLineSeparator(t *testing.T) {
	t.Parallel()
	_, err := toon.DecodeTOONDocument([]byte("op: test.op\ncount: 0"))
	if err == nil {
		t.Fatal("err=nil; want missing-separator error")
	}
	if !strings.Contains(err.Error(), "missing blank-line separator") {
		t.Errorf("err=%q; want missing-separator", err.Error())
	}
}

// TestDecodeMalformedHeaderLine pins the parseDocumentHeaders err
// arm via DecodeTOONDocument propagation (document.go:52-54 +
// document.go:115-117). A header line without a colon trips the parser.
func TestDecodeMalformedHeaderLine(t *testing.T) {
	t.Parallel()
	_, err := toon.DecodeTOONDocument([]byte("no-colon-header\n\nbody"))
	if err == nil {
		t.Fatal("err=nil; want malformed-header error")
	}
	if !strings.Contains(err.Error(), "malformed header line") {
		t.Errorf("err=%q; want malformed-header", err.Error())
	}
}

// TestDecodeHeaderCRLFBlankLineTolerated pins parseDocumentHeaders'
// `line == "" → continue` arm (document.go:111-112). A truly blank line
// can't appear in the header section (the first "\n\n" terminates it),
// but a CR-only line ("\n\r\n", which does NOT contain "\n\n") survives
// into the header split and becomes "" after the TrimRight(\r). The
// parser must skip it rather than error, preserving CRLF tolerance.
func TestDecodeHeaderCRLFBlankLineTolerated(t *testing.T) {
	t.Parallel()
	// The "\r" between format_version and the real "\n\n" separator is a
	// blank header line after CR-trim.
	doc := "op: t.op\nvariant: v1\ncount: 0\nfields: a\nformat_version: 1\n\r\n\n{}"
	got, err := toon.DecodeTOONDocument([]byte(doc))
	if err != nil {
		t.Fatalf("DecodeTOONDocument: %v (CR-only header line must be skipped, not error)", err)
	}
	if got.Op != "t.op" || got.Count != 0 {
		t.Errorf("got Op=%q Count=%d; want op=t.op count=0", got.Op, got.Count)
	}
}

// TestDecodeBadFormatVersion pins the format_version Atoi err arm
// (document.go:62-65). A non-integer format_version is rejected.
func TestDecodeBadFormatVersion(t *testing.T) {
	t.Parallel()
	doc := "op: t\nvariant: v\nformat_version: notanint\ncount: 0\nfields: \n\n{}"
	_, err := toon.DecodeTOONDocument([]byte(doc))
	if err == nil {
		t.Fatal("err=nil; want format_version-not-integer")
	}
	if !strings.Contains(err.Error(), "format_version") {
		t.Errorf("err=%q; want format_version err", err.Error())
	}
}

// TestDecodeWrongFormatVersionReturnsSentinel pins the
// `fv != 1 → ErrToonVersionUnsupported wrap` arm (document.go:66-68).
func TestDecodeWrongFormatVersionReturnsSentinel(t *testing.T) {
	t.Parallel()
	doc := "op: t\nvariant: v\nformat_version: 2\ncount: 0\nfields: \n\n{}"
	_, err := toon.DecodeTOONDocument([]byte(doc))
	if !errors.Is(err, toon.ErrToonVersionUnsupported) {
		t.Errorf("err=%v; want ErrToonVersionUnsupported wrap", err)
	}
}

// TestDecodeBadCount pins the count Atoi err arm (document.go:70-73).
func TestDecodeBadCount(t *testing.T) {
	t.Parallel()
	doc := "op: t\nvariant: v\nformat_version: 1\ncount: zero\nfields: \n\n{}"
	_, err := toon.DecodeTOONDocument([]byte(doc))
	if err == nil {
		t.Fatal("err=nil; want count-not-integer")
	}
	if !strings.Contains(err.Error(), "count") {
		t.Errorf("err=%q; want count err", err.Error())
	}
}

// TestDecodeCountZeroBodyNotSentinel pins the
// `count==0 && body != "{}" → error` arm (document.go:88-90). When the
// header says count=0 the body MUST be the literal `{}` sentinel.
func TestDecodeCountZeroBodyNotSentinel(t *testing.T) {
	t.Parallel()
	doc := "op: t\nvariant: v\nformat_version: 1\ncount: 0\nfields: \n\nnot-sentinel"
	_, err := toon.DecodeTOONDocument([]byte(doc))
	if err == nil {
		t.Fatal("err=nil; want count=0-but-body-not-{}-error")
	}
	if !strings.Contains(err.Error(), "count=0 but body is not {}") {
		t.Errorf("err=%q; want count=0-but-body-not-{}", err.Error())
	}
}

// TestDecodeShortRowPadsWithNil pins mapCSVRowToFields's
// `i >= len(row) → out[i] = nil` arm (document.go:142-144). A row with
// fewer cells than the declared fields list pads missing cells with
// nil — the decoder MUST not panic on out-of-range indexing.
func TestDecodeShortRowPadsWithNil(t *testing.T) {
	t.Parallel()
	doc := "op: t\nvariant: v\nformat_version: 1\ncount: 1\nfields: a,b,c\n\n1\n"
	got, err := toon.DecodeTOONDocument([]byte(doc))
	if err != nil {
		t.Fatalf("DecodeTOONDocument: %v", err)
	}
	if len(got.Rows) != 1 || len(got.Rows[0]) != 3 {
		t.Fatalf("rows=%v; want 1 row of 3 cols", got.Rows)
	}
	if got.Rows[0][1] != nil || got.Rows[0][2] != nil {
		t.Errorf("rows[0][1..2]=%v,%v; want nil,nil (short row)", got.Rows[0][1], got.Rows[0][2])
	}
}
