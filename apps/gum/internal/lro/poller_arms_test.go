package lro_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/lro"
)

// TestPollerTimesOutAfterFetch pins the post-fetch deadline check. The clock
// crosses TotalTimeout while the upstream Fetch is in flight, so the loop
// must report a timeout instead of sleeping for another round.
func TestPollerTimesOutAfterFetch(t *testing.T) {
	base := time.Unix(1700000000, 0)
	ticks := []time.Time{base, base, base.Add(10 * time.Minute)}
	var i int
	now := func() time.Time {
		if i < len(ticks) {
			t := ticks[i]
			i++
			return t
		}
		return ticks[len(ticks)-1]
	}

	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(context.Context, string) (*lro.Status, error) {
			return &lro.Status{Done: false}, nil
		}),
		TotalTimeout: time.Minute,
		Now:          now,
		After:        func(time.Duration) <-chan time.Time { return neverChan() },
	}

	_, err := p.Poll(context.Background(), "operations/slow")
	var timeout *lro.TimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("Poll err=%v; want *lro.TimeoutError", err)
	}
	if timeout.Elapsed != 10*time.Minute {
		t.Errorf("Elapsed=%s; want 10m0s", timeout.Elapsed)
	}
	if timeout.OperationName != "operations/slow" {
		t.Errorf("OperationName=%q; want operations/slow", timeout.OperationName)
	}
}

// TestPollerDefaultsAfterToTimeAfter pins the After default. A caller that
// supplies no sleep function still gets a working poller, so an operation
// that is already done returns without touching the real clock.
func TestPollerDefaultsAfterToTimeAfter(t *testing.T) {
	p := &lro.Poller{
		Fetcher: lro.FetcherFunc(func(context.Context, string) (*lro.Status, error) {
			return &lro.Status{Done: true, Result: "ok"}, nil
		}),
	}

	got, err := p.Poll(context.Background(), "operations/done")
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got != "ok" {
		t.Errorf("result=%v; want ok", got)
	}
}
