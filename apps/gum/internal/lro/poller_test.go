// Package lro_test — Red Team failing tests for gum-9vuq.7.
//
// Asserts the contract of the lro.Poller (§5.7 polling-loop semantics):
// 2s initial interval, 1.5× backoff, 60s ceiling, 600s timeout cap,
// *TimeoutError on expiry, OnTick progress callbacks, ctx cancellation,
// goroutine-leak safety.
//
// These tests WILL NOT COMPILE until Green creates internal/lro/poller.go
// exporting: Status, Fetcher, FetcherFunc, TimeoutError, Poller.
package lro_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/lro"
)

// closedChan returns a channel that is already closed (fires immediately).
func closedChan() <-chan time.Time {
	ch := make(chan time.Time)
	close(ch)
	return ch
}

// neverChan returns a channel that never fires.
func neverChan() <-chan time.Time {
	return make(chan time.Time) // never closed, never sent to
}

// TestPollerReturnsResultOnImmediateDone — Test A.
//
// Fetcher returns Done on the very first call. Poll must return the terminal
// result immediately without sleeping. The injected After channel never fires,
// so any attempt to sleep would block forever.
func TestPollerReturnsResultOnImmediateDone(t *testing.T) {
	want := map[string]any{"name": "op/123", "done": true}

	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(_ context.Context, _ string) (*lro.Status, error) {
			return &lro.Status{Done: true, Result: want}, nil
		}),
		After: func(_ time.Duration) <-chan time.Time {
			return neverChan() // must not be reached
		},
	}

	ctx := context.Background()
	got, err := p.Poll(ctx, "op/123")
	if err != nil {
		t.Fatalf("Poll returned unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("Poll returned nil result; want non-nil terminal result")
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("Poll result is %T; want map[string]any", got)
	}
	if m["name"] != "op/123" {
		t.Errorf("result[name]=%v; want \"op/123\"", m["name"])
	}
}

// TestPollerHonorsBackoffSchedule — Test B.
//
// Fetcher records each call; returns Done on the 5th call.
// The injected After fires immediately (closed channel) and records each
// requested duration. Asserts exactly 5 fetches and 4 intervals matching
// the 2s→3s→4.5s→6.75s geometric sequence (1.5× multiplier).
func TestPollerHonorsBackoffSchedule(t *testing.T) {
	const target = 5
	calls := 0
	var intervals []time.Duration

	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(_ context.Context, _ string) (*lro.Status, error) {
			calls++
			if calls >= target {
				return &lro.Status{Done: true, Result: "done"}, nil
			}
			return &lro.Status{Done: false}, nil
		}),
		InitialInterval: 2 * time.Second,
		BackoffFactor:   1.5,
		MaxInterval:     60 * time.Second,
		TotalTimeout:    10 * time.Minute,
		After: func(d time.Duration) <-chan time.Time {
			intervals = append(intervals, d)
			return closedChan()
		},
	}

	_, err := p.Poll(context.Background(), "ops/test-b")
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if calls != target {
		t.Errorf("fetcher called %d times; want %d", calls, target)
	}

	// 4 intervals for 5 fetches (interval fires before each non-first fetch,
	// OR after each non-terminal result — either convention is acceptable; we
	// verify the geometric sequence regardless of exact placement convention).
	want := []time.Duration{
		2 * time.Second,
		3 * time.Second,
		4*time.Second + 500*time.Millisecond, // 4.5s
		6*time.Second + 750*time.Millisecond, // 6.75s
	}
	if len(intervals) != len(want) {
		t.Fatalf("recorded %d intervals; want %d: %v", len(intervals), len(want), intervals)
	}
	for i, w := range want {
		if intervals[i] != w {
			t.Errorf("interval[%d]=%v; want %v", i, intervals[i], w)
		}
	}
}

// TestPollerCapsAt60Seconds — Test C.
//
// Uses InitialInterval=20s, BackoffFactor=2.0, MaxInterval=60s. Fetcher
// returns Done on call 10. Asserts that the recorded intervals stop growing
// at 60s (i.e., intervals beyond the third are exactly 60s).
func TestPollerCapsAt60Seconds(t *testing.T) {
	const target = 10
	calls := 0
	var intervals []time.Duration

	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(_ context.Context, _ string) (*lro.Status, error) {
			calls++
			if calls >= target {
				return &lro.Status{Done: true, Result: "done"}, nil
			}
			return &lro.Status{Done: false}, nil
		}),
		InitialInterval: 20 * time.Second,
		BackoffFactor:   2.0,
		MaxInterval:     60 * time.Second,
		TotalTimeout:    10 * time.Minute,
		After: func(d time.Duration) <-chan time.Time {
			intervals = append(intervals, d)
			return closedChan()
		},
	}

	_, err := p.Poll(context.Background(), "ops/test-c")
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if calls != target {
		t.Errorf("fetcher called %d times; want %d", calls, target)
	}
	// 9 intervals for 10 fetches.
	if len(intervals) != 9 {
		t.Fatalf("recorded %d intervals; want 9", len(intervals))
	}

	// First interval: 20s. Second: 40s. Third+: 60s (capped).
	if intervals[0] != 20*time.Second {
		t.Errorf("intervals[0]=%v; want 20s", intervals[0])
	}
	if intervals[1] != 40*time.Second {
		t.Errorf("intervals[1]=%v; want 40s", intervals[1])
	}
	for i := 2; i < len(intervals); i++ {
		if intervals[i] != 60*time.Second {
			t.Errorf("intervals[%d]=%v; want 60s (capped)", i, intervals[i])
		}
	}
}

// TestPollerTimeoutFiresAtTotalTimeout — Test D.
//
// Uses a deterministic injected Now that advances by the current interval on
// each After call, so TotalTimeout is reached within a bounded number of
// iterations. Poll must return *lro.TimeoutError.
func TestPollerTimeoutFiresAtTotalTimeout(t *testing.T) {
	const (
		initial      = 200 * time.Millisecond
		totalTimeout = 3 * time.Second
	)

	var currentInterval time.Duration
	now := time.Now()

	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(_ context.Context, _ string) (*lro.Status, error) {
			return &lro.Status{Done: false}, nil
		}),
		InitialInterval: initial,
		BackoffFactor:   1.5,
		MaxInterval:     time.Second,
		TotalTimeout:    totalTimeout,
		Now: func() time.Time {
			return now
		},
		After: func(d time.Duration) <-chan time.Time {
			currentInterval = d
			now = now.Add(currentInterval) // advance clock by the interval
			return closedChan()
		},
	}

	_, err := p.Poll(context.Background(), "ops/test-456")
	if err == nil {
		t.Fatal("Poll returned nil error; want *lro.TimeoutError")
	}

	var te *lro.TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("Poll error is %T (%v); want *lro.TimeoutError", err, err)
	}
	if te.OperationName != "ops/test-456" {
		t.Errorf("TimeoutError.OperationName=%q; want \"ops/test-456\"", te.OperationName)
	}
	if te.Elapsed < totalTimeout {
		t.Errorf("TimeoutError.Elapsed=%v; want >= %v", te.Elapsed, totalTimeout)
	}
}

// TestPollerCancellationExits — Test E.
//
// After blocks on a real hour-long timer so the poller would run forever
// without a cancel. Cancels the ctx ~20ms after starting. Poll must return
// within 1 second with context.Canceled.
func TestPollerCancellationExits(t *testing.T) {
	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(_ context.Context, _ string) (*lro.Status, error) {
			return &lro.Status{Done: false}, nil
		}),
		InitialInterval: 2 * time.Second,
		BackoffFactor:   1.5,
		MaxInterval:     60 * time.Second,
		TotalTimeout:    10 * time.Minute,
		After: func(_ time.Duration) <-chan time.Time {
			return time.After(time.Hour) // never fires in practice
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := p.Poll(ctx, "ops/test-e")
		done <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Poll returned %v; want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Poll did not exit within 1s after context cancellation")
	}
}

// TestPollerFetchErrorPropagates — Test F.
//
// When Fetcher returns an error on the first call, Poll must return that
// exact error unchanged so the handler can surface it verbatim.
func TestPollerFetchErrorPropagates(t *testing.T) {
	sentinel := errors.New("LRO_UNROUTABLE")

	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(_ context.Context, _ string) (*lro.Status, error) {
			return nil, sentinel
		}),
		After: func(_ time.Duration) <-chan time.Time {
			return neverChan()
		},
	}

	_, err := p.Poll(context.Background(), "ops/test-f")
	if !errors.Is(err, sentinel) {
		t.Errorf("Poll returned %v; want sentinel LRO_UNROUTABLE error", err)
	}
}

// TestPollerOnTickFiresEachLoop — Test G.
//
// Fetcher returns Done on call 4. OnTick must fire exactly 3 times (after
// each of the 3 non-terminal results). Elapsed values must be monotonically
// non-decreasing.
func TestPollerOnTickFiresEachLoop(t *testing.T) {
	calls := 0
	var ticks []time.Duration

	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(_ context.Context, _ string) (*lro.Status, error) {
			calls++
			if calls >= 4 {
				return &lro.Status{Done: true, Result: "done"}, nil
			}
			return &lro.Status{Done: false}, nil
		}),
		InitialInterval: 2 * time.Second,
		BackoffFactor:   1.5,
		MaxInterval:     60 * time.Second,
		TotalTimeout:    10 * time.Minute,
		After: func(_ time.Duration) <-chan time.Time {
			return closedChan()
		},
		OnTick: func(elapsed time.Duration) {
			ticks = append(ticks, elapsed)
		},
	}

	_, err := p.Poll(context.Background(), "ops/test-g")
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}

	if len(ticks) != 3 {
		t.Errorf("OnTick fired %d times; want 3 (once after each non-terminal fetch)", len(ticks))
	}

	// Elapsed must be monotonically non-decreasing.
	for i := 1; i < len(ticks); i++ {
		if ticks[i] < ticks[i-1] {
			t.Errorf("ticks[%d]=%v < ticks[%d]=%v; want non-decreasing", i, ticks[i], i-1, ticks[i-1])
		}
	}
}

// TestPollerGoroutineNoLeak — Test H.
//
// Uses goleak.VerifyNone to assert that no goroutine spawned by the Poller
// survives after Poll returns following a ctx cancellation.
func TestPollerGoroutineNoLeak(t *testing.T) {
	defer goleak.VerifyNone(t)

	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(ctx context.Context, _ string) (*lro.Status, error) {
			// Block until ctx is done so we can exercise cancellation paths.
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Hour):
				return &lro.Status{Done: false}, nil
			}
		}),
		InitialInterval: 2 * time.Second,
		BackoffFactor:   1.5,
		MaxInterval:     60 * time.Second,
		TotalTimeout:    10 * time.Minute,
		After: func(_ time.Duration) <-chan time.Time {
			return closedChan()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Poll(ctx, "ops/test-h") //nolint:errcheck
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Poll goroutine did not exit within 2s after cancellation")
	}
	// goleak.VerifyNone fires in the deferred call above.
}
