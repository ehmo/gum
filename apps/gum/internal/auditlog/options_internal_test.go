package auditlog

import (
	"testing"
	"time"
)

// TestWithClockNilResets pins the nil-clock branch: passing nil to
// WithClock must reset the writer's clock to time.Now, not nil. A
// regression that left now=nil would NPE on the first Append.
func TestWithClockNilResets(t *testing.T) {
	w := &Writer{}
	WithClock(nil)(w)
	if w.now == nil {
		t.Fatalf("WithClock(nil): w.now is nil; want fallback to time.Now")
	}
	// Smoke test: invoking the returned function returns a recent time.
	got := w.now()
	if time.Since(got) > time.Second {
		t.Errorf("w.now() returned %v; want recent (system clock)", got)
	}
}

// TestWithBufferedChannelZeroIsNoop pins the n<=0 short-circuit: the
// channel must NOT be allocated, leaving Writer in synchronous mode.
// The Append path checks w.ch == nil to decide sync vs. async, so a
// regression that allocated a zero-cap channel would deadlock immediately.
func TestWithBufferedChannelZeroIsNoop(t *testing.T) {
	w := &Writer{}
	WithBufferedChannel(0)(w)
	if w.ch != nil {
		t.Errorf("WithBufferedChannel(0): ch was allocated; want nil")
	}
	WithBufferedChannel(-5)(w)
	if w.ch != nil {
		t.Errorf("WithBufferedChannel(-5): ch was allocated; want nil")
	}
}

// TestWithDrainTimeoutClamping pins all three branches: negative
// values reset to DefaultDrainTimeout; over-MaxDrainTimeout clamps
// down; in-bounds values pass through.
func TestWithDrainTimeoutClamping(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"negative_resets_to_default", -1, DefaultDrainTimeout},
		{"zero_passes_through", 0, 0},
		{"in_bounds_passes_through", 500 * time.Millisecond, 500 * time.Millisecond},
		{"over_max_clamps_down", MaxDrainTimeout + time.Second, MaxDrainTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &Writer{}
			WithDrainTimeout(tc.in)(w)
			if w.drainTimeout != tc.want {
				t.Errorf("drainTimeout=%v; want %v", w.drainTimeout, tc.want)
			}
		})
	}
}
