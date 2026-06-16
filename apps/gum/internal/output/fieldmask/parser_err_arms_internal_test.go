package fieldmask

import (
	"strings"
	"testing"
)

// TestParserParseIdentAtEOFReportsEOF pins parseIdent's
// `got == 0 → "expected identifier ... got EOF"` arm (parser.go:66-68).
// Unreachable through Parse (parseField guards EOF before calling
// parseIdent in every callsite chain), but the EOF distinguishability
// is required for any future caller that drives the parser directly —
// e.g. an incremental parser, REPL completion, or grammar fuzzing.
// Pinning it via internal test keeps the "EOF vs unexpected-byte"
// distinction load-bearing.
func TestParserParseIdentAtEOFReportsEOF(t *testing.T) {
	p := &parser{s: ""}
	_, err := p.parseIdent()
	if err == nil {
		t.Fatal("parseIdent on empty input returned nil err; want EOF surface")
	}
	if !strings.Contains(err.Error(), "EOF") {
		t.Errorf("err=%q; want 'EOF' marker (distinguish from unexpected-byte)", err)
	}
}

// TestParseTopLevelTrailingGarbageReportsUnexpectedChar pins
// parseMask's `!nested && p.pos != len(p.s) → unexpected character`
// arm (parser.go:104-106). Reached when the top-level mask parses a
// valid first field but trailing input remains (no comma to continue).
// Example: "foo bar" — parseField consumes "foo", the loop sees ' '
// (not ','), exits, then this arm catches the unconsumed space rather
// than silently truncating the user's mask.
func TestParseTopLevelTrailingGarbageReportsUnexpectedChar(t *testing.T) {
	_, err := Parse("foo bar")
	if err == nil {
		t.Fatal("Parse(\"foo bar\")=nil err; want trailing-char surface")
	}
	if !strings.Contains(err.Error(), "unexpected character") {
		t.Errorf("err=%q; want 'unexpected character' (not silently truncated)", err)
	}
}

// TestParseNestedMaskInnerErrorPropagates pins parseField's
// `parseMask err → return err` arm (parser.go:137-139). Reached when
// the inner mask inside parens errs out — e.g., a doubled comma —
// and parseField must propagate without wrapping (the inner err
// already names the position, double-wrap would be noise).
func TestParseNestedMaskInnerErrorPropagates(t *testing.T) {
	_, err := Parse("a(b,,c)")
	if err == nil {
		t.Fatal("Parse(\"a(b,,c)\")=nil err; want inner doubled-comma surface")
	}
	// The inner parseMask emits "unexpected ... after comma".
	if !strings.Contains(err.Error(), "after comma") {
		t.Errorf("err=%q; want inner 'after comma' err (parent didn't double-wrap)", err)
	}
}

// TestMaskStringSortsWildcardLast pins serializeNodes's
// `a.wildcard != b.wildcard → return b.wildcard` arm (mask.go:117-119).
// Reached when a mask mixes a wildcard and named fields at the same
// level — the wildcard MUST sort last so Parse(m.String()) is stable
// regardless of parse order. Without the explicit wildcard-last
// branch, the comparator would fall through to the name-sort which
// would interleave "*" amongst names (since "*" < "a" lexically).
func TestMaskStringSortsWildcardLast(t *testing.T) {
	m, err := Parse("*,foo,bar")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Both possible name-order outcomes put "*" last.
	got := m.String()
	want := "bar,foo,*"
	if got != want {
		t.Errorf("String()=%q; want %q (wildcard sorts last)", got, want)
	}
}

// TestParseUnmatchedOpenParenWrapsExpectErr pins parseField's
// `expect ')' err → return wrapped err` arm (parser.go:140-142).
// Reached when '(' opens a sub-mask, the inner mask parses cleanly,
// but EOF arrives before the closing ')'. The wrap label "unmatched
// '(' for field <name>" gives operators the offending field name —
// without the wrap, the raw expect-err only reports position.
func TestParseUnmatchedOpenParenWrapsExpectErr(t *testing.T) {
	_, err := Parse("a(b")
	if err == nil {
		t.Fatal("Parse(\"a(b\")=nil err; want unmatched-paren wrap")
	}
	if !strings.Contains(err.Error(), "unmatched '(' for field") {
		t.Errorf("err=%q; want 'unmatched (' for field' wrap", err)
	}
	if !strings.Contains(err.Error(), `"a"`) {
		t.Errorf("err=%q; want offending field name 'a' in wrap", err)
	}
}
