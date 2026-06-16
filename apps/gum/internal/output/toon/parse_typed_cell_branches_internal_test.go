package toon

import "testing"

// TestParseTypedIntCellBadInputReturnsNilPointer pins parseTypedIntCell's
// `strconv.ParseInt err → (*int64)(nil)` arm (typed.go:192-194). Per
// the docstring, unparseable input falls back to a typed-nil pointer
// (not a Go untyped nil), so a downstream JSON encoder still emits
// `null` rather than skipping the field.
func TestParseTypedIntCellBadInputReturnsNilPointer(t *testing.T) {
	t.Parallel()
	got := parseTypedIntCell("not-a-number", false)
	p, ok := got.(*int64)
	if !ok {
		t.Fatalf("parseTypedIntCell(\"not-a-number\") = %T; want *int64", got)
	}
	if p != nil {
		t.Errorf("*int64 = %v; want nil pointer", *p)
	}
}

// TestParseTypedFloatCellBadInputReturnsNilPointer pins
// parseTypedFloatCell's `strconv.ParseFloat err → (*float64)(nil)` arm
// (typed.go:205-207).
func TestParseTypedFloatCellBadInputReturnsNilPointer(t *testing.T) {
	t.Parallel()
	got := parseTypedFloatCell("not-a-float", false)
	p, ok := got.(*float64)
	if !ok {
		t.Fatalf("parseTypedFloatCell(\"not-a-float\") = %T; want *float64", got)
	}
	if p != nil {
		t.Errorf("*float64 = %v; want nil pointer", *p)
	}
}
