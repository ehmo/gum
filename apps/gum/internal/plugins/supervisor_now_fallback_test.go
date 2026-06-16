package plugins

import (
	"testing"
	"time"
)

// TestNewSupervisorNilClockFallsBackToTimeNow pins the `now == nil`
// arm of NewSupervisor: callers that don't inject a clock MUST still
// get a supervisor whose now() is time.Now (real wall clock), not a
// zero-value func panic.
func TestNewSupervisorNilClockFallsBackToTimeNow(t *testing.T) {
	sup := NewSupervisor(nil, nil, nil)
	if sup == nil {
		t.Fatal("NewSupervisor returned nil")
	}
	if sup.now == nil {
		t.Fatal("sup.now still nil after fallback; want time.Now")
	}
	// Sanity: the returned clock yields a recent timestamp (≤ a few
	// seconds drift from wall time when the test ran).
	if got := sup.now(); time.Since(got) > time.Minute {
		t.Errorf("sup.now()=%s; want close to wall time (drift > 1m)", got)
	}
}
