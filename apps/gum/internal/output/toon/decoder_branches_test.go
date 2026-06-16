package toon_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/toon"
)

// TestDecodeHeterogeneousMalformedJSONWraps pins encoder.go:403-405 —
// when a `# heterogeneous` prefix is followed by JSON that fails
// json.Unmarshal, Decode MUST wrap the err with "toon: decode
// heterogeneous:" so operators see which branch failed (rather than a
// bare JSON error message).
func TestDecodeHeterogeneousMalformedJSONWraps(t *testing.T) {
	t.Parallel()
	in := []byte("# heterogeneous\n{this is not json}\n")
	_, err := toon.Decode(in)
	if err == nil {
		t.Fatal("Decode(# heterogeneous + malformed JSON)=nil err; want wrap")
	}
	if !strings.Contains(err.Error(), "decode heterogeneous:") {
		t.Errorf("err=%q; want 'decode heterogeneous:' wrap", err)
	}
}

// TestDecodeHeadersShortRowFillsNil pins encoder.go:469-471 — when the
// `# headers:` format has data rows shorter than the header count, the
// missing trailing columns MUST be filled with nil so the resulting
// map shape matches the header schema.
func TestDecodeHeadersShortRowFillsNil(t *testing.T) {
	t.Parallel()
	// 3 headers, 2-column data row → third column gets nil.
	in := []byte("# headers: a,b,c\n1,2\n")
	v, err := toon.Decode(in)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rows, ok := v.([]any)
	if !ok {
		t.Fatalf("Decode result type=%T; want []any", v)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows)=%d; want 1", len(rows))
	}
	m, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("row[0] type=%T; want map[string]any", rows[0])
	}
	if got, present := m["c"]; !present || got != nil {
		t.Errorf("m[c]=%v present=%v; want nil/present", got, present)
	}
}

// TestDecodeCSVWithCarriageReturnsTolerated pins parseDocumentCSV's
// CR-skipping arm.
// CRLF line endings are common in clipboard/Windows pastes; the decoder
// MUST tolerate them rather than treating \r as a literal field byte.
func TestDecodeCSVWithCarriageReturnsTolerated(t *testing.T) {
	t.Parallel()
	in := []byte("a,b\r\n1,2\r\n3,4\r\n")
	v, err := toon.Decode(in)
	if err != nil {
		t.Fatalf("Decode CRLF: %v", err)
	}
	rows, ok := v.([]any)
	if !ok {
		t.Fatalf("Decode result type=%T; want []any", v)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows)=%d; want 2", len(rows))
	}
	// First row's "a" cell MUST be 1 (numeric), not "1\r" — proves CR was stripped.
	first := rows[0].(map[string]any)
	if got := first["a"]; got != 1.0 {
		t.Errorf("rows[0].a=%v (%T); want 1.0 (CR-stripping should not leave \\r in cell)", got, got)
	}
}

// TestDecodeCSVCellLiteralFalseReturnsFalseBool pins encoder.go:580 —
// decodeCSVCell's `case "false": return false` arm. Without this, a
// CSV cell containing the literal text "false" would decode as the
// string "false" rather than the bool, breaking JSON round-trip.
func TestDecodeCSVCellLiteralFalseReturnsFalseBool(t *testing.T) {
	t.Parallel()
	in := []byte("flag\nfalse\ntrue\n")
	v, err := toon.Decode(in)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rows, ok := v.([]any)
	if !ok {
		t.Fatalf("Decode result type=%T; want []any", v)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows)=%d; want 2", len(rows))
	}
	first := rows[0].(map[string]any)
	got, ok := first["flag"].(bool)
	if !ok {
		t.Fatalf("rows[0].flag type=%T; want bool", first["flag"])
	}
	if got != false {
		t.Errorf("rows[0].flag=%v; want false", got)
	}
}
