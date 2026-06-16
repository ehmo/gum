package mcp

import "testing"

// TestConvenienceToolABIUnknownReturnsNil pins the negative branch:
// callers route on a nil return to "not a convenience tool" rather
// than walking the ABI table themselves. A stray panic or a fabricated
// zero-value ConvenienceABI would silently break routing for unknown
// tool names that the user might typo.
func TestConvenienceToolABIUnknownReturnsNil(t *testing.T) {
	if got := ConvenienceToolABI("nope_not_a_tool"); got != nil {
		t.Errorf("unknown name returned %+v; want nil", got)
	}
	if got := ConvenienceToolABI(""); got != nil {
		t.Errorf("empty name returned %+v; want nil", got)
	}
}

// TestConvenienceToolABIReturnsCopy: the helper returns a *copy*, so a
// caller mutating the result must NOT poison the next lookup. Locks in
// the immutability contract documented above the function.
func TestConvenienceToolABIReturnsCopy(t *testing.T) {
	first := ConvenienceToolABI("flights_search")
	if first == nil {
		t.Fatalf("ConvenienceToolABI(flights_search) returned nil; setup error")
	}
	first.OpID = "POISONED"

	second := ConvenienceToolABI("flights_search")
	if second == nil {
		t.Fatalf("second lookup returned nil")
	}
	if second.OpID == "POISONED" {
		t.Errorf("mutation leaked into the table: second.OpID=%q", second.OpID)
	}
}
