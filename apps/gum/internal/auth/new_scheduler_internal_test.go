package auth

import (
	"testing"
	"time"
)

// TestNewSchedulerDefaultsApplied pins the two zero-value fallbacks:
// StaleAfter==0 → 24h, Now==nil → time.Now. Without these the canary
// scheduler would compare against a zero time.Time and report every
// scope as stale.
func TestNewSchedulerDefaultsApplied(t *testing.T) {
	s := NewScheduler(SchedulerConfig{})
	if s == nil {
		t.Fatal("got nil")
	}
	if s.cfg.StaleAfter != 24*time.Hour {
		t.Errorf("StaleAfter=%v; want 24h default", s.cfg.StaleAfter)
	}
	if s.cfg.Now == nil {
		t.Fatal("Now: nil; want time.Now fallback")
	}
	if s.cfg.Now().IsZero() {
		t.Error("Now() returned zero time")
	}
}

// TestNewSchedulerHonoursExplicitConfig pins the non-default path:
// caller-supplied StaleAfter and Now must round-trip verbatim.
func TestNewSchedulerHonoursExplicitConfig(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewScheduler(SchedulerConfig{
		StaleAfter: 5 * time.Minute,
		Now:        func() time.Time { return fixed },
	})
	if s.cfg.StaleAfter != 5*time.Minute {
		t.Errorf("StaleAfter=%v; want 5m", s.cfg.StaleAfter)
	}
	if got := s.cfg.Now(); !got.Equal(fixed) {
		t.Errorf("Now()=%v; want %v", got, fixed)
	}
}
