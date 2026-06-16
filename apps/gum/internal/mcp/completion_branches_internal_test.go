package mcp

import "testing"

// TestCompletionOpIDsNilSnapshotReturnsNil pins completionOpIDs's
// `s.snapshot == nil → return nil` arm (completion.go:73-75). A
// Server constructed before SetSnapshot fires must not panic on
// completion lookups.
func TestCompletionOpIDsNilSnapshotReturnsNil(t *testing.T) {
	s := &Server{}
	if got := s.completionOpIDs(); got != nil {
		t.Errorf("got %v; want nil for missing snapshot", got)
	}
}

// TestCompletionVariantIDsNilSnapshotReturnsNil pins
// completionVariantIDs's `s.snapshot == nil → return nil` arm
// (completion.go:88-90). Same fresh-server guarantee as above.
func TestCompletionVariantIDsNilSnapshotReturnsNil(t *testing.T) {
	s := &Server{}
	if got := s.completionVariantIDs(); got != nil {
		t.Errorf("got %v; want nil for missing snapshot", got)
	}
}

// TestCapCompletionValuesTruncatesAtMax pins capCompletionValues's
// `hasMore → values = values[:cap]` arm (completion.go:169-171).
// Reached when the input exceeds completionMaxValues (50) — the
// returned CompleteResult MUST truncate and set HasMore=true while
// Total reflects the pre-truncation count.
func TestCapCompletionValuesTruncatesAtMax(t *testing.T) {
	in := make([]string, completionMaxValues+5)
	for i := range in {
		in[i] = "v"
	}
	res := capCompletionValues(in)
	if len(res.Completion.Values) != completionMaxValues {
		t.Errorf("len(Values)=%d; want %d (cap)", len(res.Completion.Values), completionMaxValues)
	}
	if !res.Completion.HasMore {
		t.Error("HasMore=false; want true on truncation")
	}
	if res.Completion.Total != completionMaxValues+5 {
		t.Errorf("Total=%d; want pre-truncation count %d", res.Completion.Total, completionMaxValues+5)
	}
}

// TestServerConvenienceToolNamesNilReturnsEmpty pins
// Server.ConvenienceToolNames's `s.convenienceToolNames == nil →
// return []string{}` arm (server.go:237-239). A freshly-constructed
// Server (before registerConvenienceTools fires) must report an
// empty list rather than nil so callers can range without a guard.
func TestServerConvenienceToolNamesNilReturnsEmpty(t *testing.T) {
	s := &Server{}
	got := s.ConvenienceToolNames()
	if got == nil {
		t.Error("got nil; want empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len=%d; want 0", len(got))
	}
}
