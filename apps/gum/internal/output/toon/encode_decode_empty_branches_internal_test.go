package toon

import (
	"testing"
)

// TestEncodeMapAllValuesSkippedEmitsEmptyObject pins encoder.go:243-246
// — the `!hasAny → return "{}\n"` arm of encodeMap. When OmitZeroCounts
// is set and every value in the map is a zero-int/float, the encode loop
// skips them all; hasAny stays false and the function MUST emit the
// empty-object sentinel rather than a blank string (which Decode would
// read back as nil, not an empty map).
func TestEncodeMapAllValuesSkippedEmitsEmptyObject(t *testing.T) {
	m := map[string]any{"a": 0, "b": 0, "c": int64(0)}
	got, err := EncodeWithOptions(m, EncoderOptions{OmitZeroCounts: true})
	if err != nil {
		t.Fatalf("EncodeWithOptions: %v", err)
	}
	if string(got) != "{}\n" {
		t.Errorf("got=%q; want %q (all-skipped map must emit empty-object sentinel)", got, "{}\n")
	}
}

// TestDecodeCSVTableEmptyInputReturnsEmptyArray pins encoder.go:482-484
// — `len(rows) == 0 → return []any{}`. parseDocumentCSV("") yields zero
// rows; decodeCSVTable MUST return a non-nil empty slice so callers can
// distinguish "valid empty table" from a decode error.
func TestDecodeCSVTableEmptyInputReturnsEmptyArray(t *testing.T) {
	got, err := decodeCSVTable("")
	if err != nil {
		t.Fatalf("decodeCSVTable(\"\"): %v", err)
	}
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("got %T; want []any", got)
	}
	if arr == nil {
		t.Error("got nil slice; want non-nil empty []any")
	}
	if len(arr) != 0 {
		t.Errorf("len=%d; want 0", len(arr))
	}
}
