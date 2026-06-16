package toon

import (
	"math"
	"testing"
)

// TestDecodeDocumentCellBranches pins each return arm of decodeDocumentCell:
// the three reserved literals decode to typed Go values, parseable numbers
// decode as float64, and anything else falls through as the raw string.
func TestDecodeDocumentCellBranches(t *testing.T) {
	t.Run("null_returns_nil", func(t *testing.T) {
		if got := decodeDocumentCell("null"); got != nil {
			t.Errorf("got=%v; want nil", got)
		}
	})
	t.Run("true_returns_bool_true", func(t *testing.T) {
		if got := decodeDocumentCell("true"); got != true {
			t.Errorf("got=%v; want true", got)
		}
	})
	t.Run("false_returns_bool_false", func(t *testing.T) {
		if got := decodeDocumentCell("false"); got != false {
			t.Errorf("got=%v; want false", got)
		}
	})
	t.Run("integer_string_parses_as_float64", func(t *testing.T) {
		got := decodeDocumentCell("42")
		f, ok := got.(float64)
		if !ok {
			t.Fatalf("got %T %v; want float64", got, got)
		}
		if f != 42 {
			t.Errorf("got=%v; want 42", f)
		}
	})
	t.Run("decimal_string_parses_as_float64", func(t *testing.T) {
		got := decodeDocumentCell("3.14")
		f, ok := got.(float64)
		if !ok {
			t.Fatalf("got %T %v; want float64", got, got)
		}
		if math.Abs(f-3.14) > 1e-9 {
			t.Errorf("got=%v; want 3.14", f)
		}
	})
	t.Run("non_numeric_string_passes_through", func(t *testing.T) {
		got := decodeDocumentCell("hello world")
		if got != "hello world" {
			t.Errorf("got=%v; want 'hello world'", got)
		}
	})
}
