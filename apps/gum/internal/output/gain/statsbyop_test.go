package gain

import (
	"testing"
	"time"
)

// TestStatsByOp verifies per-op aggregation groups entries by op_id and
// computes independent stats for each (review gum-y5wb wiring `gum gain
// --by-op`).
func TestStatsByOp(t *testing.T) {
	l := &Ledger{entries: []Entry{
		{OpID: "gmail.users.messages.list", RawTokens: 100, ShapedTokens: 40},
		{OpID: "gmail.users.messages.list", RawTokens: 200, ShapedTokens: 50},
		{OpID: "calendar.events.list", RawTokens: 80, ShapedTokens: 30},
	}}

	got := l.StatsByOp(time.Time{}, time.Time{})
	if len(got) != 2 {
		t.Fatalf("StatsByOp groups = %d; want 2 (%v)", len(got), got)
	}
	gmail, ok := got["gmail.users.messages.list"]
	if !ok {
		t.Fatalf("missing gmail group: %v", got)
	}
	if gmail.TotalCalls != 2 {
		t.Errorf("gmail TotalCalls = %d; want 2", gmail.TotalCalls)
	}
	// 100-40 + 200-50 = 210
	if gmail.TotalTokensSaved != 210 {
		t.Errorf("gmail TotalTokensSaved = %d; want 210", gmail.TotalTokensSaved)
	}
	cal := got["calendar.events.list"]
	if cal.TotalCalls != 1 || cal.TotalTokensSaved != 50 {
		t.Errorf("calendar stats = %+v; want 1 call / 50 saved", cal)
	}
}
