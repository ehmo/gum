package plugins

import (
	"testing"
	"time"
)

// TestTransferOptionsNowFallsBackToTimeNow pins the nil-Now arm:
// when no clock hook is supplied, the helper MUST delegate to
// time.Now so production callers don't get a zero time.
func TestTransferOptionsNowFallsBackToTimeNow(t *testing.T) {
	before := time.Now().Add(-time.Second)
	opts := TransferOptions{} // Now: nil
	got := opts.now()
	after := time.Now().Add(time.Second)
	if got.Before(before) || got.After(after) {
		t.Errorf("now()=%v; want within [%v, %v]", got, before, after)
	}
}

// TestTransferOptionsNowUsesInjectedClock pins the non-nil-Now arm:
// when a fake clock is supplied, the helper MUST return its value so
// tests can pin timestamps deterministically.
func TestTransferOptionsNowUsesInjectedClock(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	opts := TransferOptions{Now: func() time.Time { return fixed }}
	if got := opts.now(); !got.Equal(fixed) {
		t.Errorf("now()=%v; want %v", got, fixed)
	}
}
