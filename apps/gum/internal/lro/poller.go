// Package lro implements the §5.7 long-running-operation poller used by gum.poll.
//
// The package is intentionally narrow: it owns the polling LOOP (intervals,
// backoff, timeout, cancellation, progress callbacks) and abstracts upstream
// state behind the Fetcher interface. Routing/dispatch — picking the right
// Operations endpoint per service prefix — is OUT of scope here; that lives in
// the future gen-catalog-emitted lroServiceByPrefix table (§5.7 fallback).
package lro

import (
	"context"
	"fmt"
	"time"
)

// Status is one fetched view of an upstream Operation.
type Status struct {
	Done   bool
	Result any
}

// Fetcher reads upstream LRO state for operationName.
type Fetcher interface {
	Fetch(ctx context.Context, operationName string) (*Status, error)
}

// FetcherFunc adapts an ordinary function to Fetcher.
type FetcherFunc func(ctx context.Context, operationName string) (*Status, error)

func (f FetcherFunc) Fetch(ctx context.Context, operationName string) (*Status, error) {
	return f(ctx, operationName)
}

// TimeoutError is returned when polling exceeds Poller.TotalTimeout.
type TimeoutError struct {
	OperationName string
	Elapsed       time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("lro: poll timeout after %s for operation %q", e.Elapsed, e.OperationName)
}

// Default knobs (spec §4.1 lines 336-339).
const (
	DefaultInitialInterval = 2 * time.Second
	DefaultBackoffFactor   = 1.5
	DefaultMaxInterval     = 60 * time.Second
	DefaultTotalTimeout    = 10 * time.Minute
)

// Poller drives the §5.7 polling loop. Zero-value-safe: all fields default
// to spec values when left unset.
type Poller struct {
	Fetcher         Fetcher
	InitialInterval time.Duration
	BackoffFactor   float64
	MaxInterval     time.Duration
	TotalTimeout    time.Duration
	Now             func() time.Time
	After           func(time.Duration) <-chan time.Time
	// OnTick fires after each non-terminal Fetch with elapsed time from start.
	// Use for emitting MCP progress notifications.
	OnTick func(elapsed time.Duration)
}

func (p *Poller) defaults() {
	if p.InitialInterval == 0 {
		p.InitialInterval = DefaultInitialInterval
	}
	if p.BackoffFactor == 0 {
		p.BackoffFactor = DefaultBackoffFactor
	}
	if p.MaxInterval == 0 {
		p.MaxInterval = DefaultMaxInterval
	}
	if p.TotalTimeout == 0 {
		p.TotalTimeout = DefaultTotalTimeout
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.After == nil {
		p.After = time.After
	}
}

// Poll runs the loop until Done, error, ctx cancel, or TotalTimeout.
func (p *Poller) Poll(ctx context.Context, operationName string) (any, error) {
	p.defaults()
	start := p.Now()
	interval := p.InitialInterval
	for {
		// Check the deadline BEFORE fetching so a sleep that pushed us past
		// TotalTimeout doesn't trigger one extra (quota-consuming) upstream Fetch
		// after the deadline. The first iteration always passes (elapsed≈0).
		if elapsed := p.Now().Sub(start); elapsed >= p.TotalTimeout {
			return nil, &TimeoutError{OperationName: operationName, Elapsed: elapsed}
		}
		st, err := p.Fetcher.Fetch(ctx, operationName)
		if err != nil {
			return nil, err
		}
		if st != nil && st.Done {
			return st.Result, nil
		}
		elapsed := p.Now().Sub(start)
		if p.OnTick != nil {
			p.OnTick(elapsed)
		}
		if elapsed >= p.TotalTimeout {
			return nil, &TimeoutError{OperationName: operationName, Elapsed: elapsed}
		}
		// sleep
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.After(interval):
		}
		// backoff
		next := time.Duration(float64(interval) * p.BackoffFactor)
		if next > p.MaxInterval {
			next = p.MaxInterval
		}
		interval = next
	}
}
