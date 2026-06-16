package adapters

import (
	"testing"
	"time"
)

// TestFamilyGatePauseZeroDurationDefaults pins
// code_risor_parallel.go:195-197 — pause's `d <= 0 → d =
// parallel429DefaultRetryAfter` guard. The Risor parallel worker never
// calls pause with d=0 (line 148 short-circuits on pause > 0), so this
// arm only protects direct callers from accidentally clearing the
// gate. Without the guard, a stray pause(family, 0) would set
// pausedUntil = now (effectively a no-op), defeating the rate-limit
// back-off.
func TestFamilyGatePauseZeroDurationDefaults(t *testing.T) {
	t.Parallel()
	g := newFamilyGate()
	before := time.Now()
	g.pause("gmail.users.messages", 0)
	g.mu.Lock()
	until, ok := g.pausedUntil["gmail.users.messages"]
	g.mu.Unlock()
	if !ok {
		t.Fatal("pausedUntil missing entry for family after pause(0)")
	}
	// Must extend by at least the default retry-after, not by 0.
	if until.Sub(before) < parallel429DefaultRetryAfter/2 {
		t.Errorf("until-before=%v; want >= %v (zero-duration must default, not no-op)", until.Sub(before), parallel429DefaultRetryAfter/2)
	}
}

// TestFamilyGatePauseNegativeDurationDefaults pins the same arm via
// the `d < 0` branch of `d <= 0`. A negative retry-after is just as
// invalid as zero and MUST not produce an in-past pausedUntil that
// the worker reads as "no pause".
func TestFamilyGatePauseNegativeDurationDefaults(t *testing.T) {
	t.Parallel()
	g := newFamilyGate()
	before := time.Now()
	g.pause("calendar.events", -5*time.Second)
	g.mu.Lock()
	until := g.pausedUntil["calendar.events"]
	g.mu.Unlock()
	if until.Before(before) {
		t.Errorf("until=%v before before=%v; negative-duration must default to future, not past", until, before)
	}
}
