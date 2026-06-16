package fieldmask

import (
	"strings"
	"testing"
)

// TestParserExpectShapes pins the three outcomes of parser.expect:
// match (consumed), EOF mismatch (formatted with EOF marker), non-EOF
// mismatch (formatted with the actual byte). The branches stay
// distinguishable so callers can surface useful errors at the input
// position rather than a generic "syntax error".
func TestParserExpectShapes(t *testing.T) {
	t.Run("match_consumes", func(t *testing.T) {
		p := &parser{s: ","}
		if err := p.expect(','); err != nil {
			t.Fatalf("expect: %v", err)
		}
		if p.pos != 1 {
			t.Errorf("pos=%d; want 1 (byte consumed)", p.pos)
		}
	})

	t.Run("eof_mismatch", func(t *testing.T) {
		p := &parser{s: ""}
		err := p.expect(')')
		if err == nil {
			t.Fatal("expect at EOF returned nil error")
		}
		if !strings.Contains(err.Error(), "EOF") {
			t.Errorf("err=%q; want 'EOF' marker", err)
		}
	})

	t.Run("byte_mismatch", func(t *testing.T) {
		p := &parser{s: "x"}
		err := p.expect(')')
		if err == nil {
			t.Fatal("expect on mismatch returned nil error")
		}
		if strings.Contains(err.Error(), "EOF") {
			t.Errorf("err=%q; should report actual byte, not EOF", err)
		}
		// %q on a single byte renders 'x' (single quotes), not "x".
		if !strings.Contains(err.Error(), "'x'") {
			t.Errorf("err=%q; want actual byte 'x' in message", err)
		}
	})
}
