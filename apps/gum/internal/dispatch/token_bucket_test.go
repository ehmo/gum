package dispatch

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestTokenBucketStepNilBucketNoop verifies the rate-limiter step is a no-op
// when no TokenBucket is wired into the dispatcher.
// Acceptance: nil tokenBucket is a no-op.
func TestTokenBucketStepNilBucketNoop(t *testing.T) {
	d := &dispatcher{}
	if err := d.tokenBucketStep(t.Context(), &Invocation{OpID: "any"}, &ResolvedVariant{}); err != nil {
		t.Fatalf("nil bucket: unexpected err: %v", err)
	}
}

// TestTokenBucketStepUsesServiceFamily verifies Wait is called with the op's
// service_family (from catalog) — so per-family limits operate independently.
// Acceptance: Wait called with correct service-family derived from variant.
func TestTokenBucketStepUsesServiceFamily(t *testing.T) {
	tb := &captureBucket{}
	snap := &catalog.Catalog{Ops: []catalog.Op{
		{OpID: "gmail.messages.list", ServiceFamily: "gmail"},
	}}
	d := &dispatcher{snapshot: snap, tokenBucket: tb}

	err := d.tokenBucketStep(t.Context(), &Invocation{OpID: "gmail.messages.list"}, &ResolvedVariant{OpID: "gmail.messages.list"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if tb.calls != 1 {
		t.Fatalf("Wait calls=%d want 1", tb.calls)
	}
	if tb.gotOpID != "gmail.messages.list" {
		t.Errorf("opID=%q want gmail.messages.list", tb.gotOpID)
	}
	if tb.gotCredsID != "gmail" {
		t.Errorf("credsID=%q want service-family \"gmail\"", tb.gotCredsID)
	}
}

// TestTokenBucketStepContextCancellationPropagates verifies a cancelled
// context unblocks Wait and the error reaches the caller — required for
// step-6 short-circuit per spec §3.1.
// Acceptance: context cancellation returns error immediately.
func TestTokenBucketStepContextCancellationPropagates(t *testing.T) {
	tb := &blockingBucket{}
	snap := &catalog.Catalog{Ops: []catalog.Op{
		{OpID: "drive.files.list", ServiceFamily: "drive"},
	}}
	d := &dispatcher{snapshot: snap, tokenBucket: tb}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := d.tokenBucketStep(ctx, &Invocation{OpID: "drive.files.list"}, &ResolvedVariant{OpID: "drive.files.list"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

// TestTokenBucketStepPerFamilyIndependence verifies concurrent calls for
// different service families do not contend on the same bucket key.
// Acceptance: per-family limits are independent (load-test scaffold).
func TestTokenBucketStepPerFamilyIndependence(t *testing.T) {
	tb := &captureBucket{}
	snap := &catalog.Catalog{Ops: []catalog.Op{
		{OpID: "gmail.x", ServiceFamily: "gmail"},
		{OpID: "drive.x", ServiceFamily: "drive"},
	}}
	d := &dispatcher{snapshot: snap, tokenBucket: tb}

	var wg sync.WaitGroup
	for _, op := range []string{"gmail.x", "drive.x", "gmail.x", "drive.x"} {
		op := op
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.tokenBucketStep(t.Context(), &Invocation{OpID: op}, &ResolvedVariant{OpID: op})
		}()
	}
	wg.Wait()

	tb.mu.Lock()
	defer tb.mu.Unlock()
	if tb.calls != 4 {
		t.Errorf("calls=%d want 4", tb.calls)
	}
	if tb.familyCalls["gmail"] != 2 {
		t.Errorf("gmail family calls=%d want 2", tb.familyCalls["gmail"])
	}
	if tb.familyCalls["drive"] != 2 {
		t.Errorf("drive family calls=%d want 2", tb.familyCalls["drive"])
	}
}

// TestTokenBucketStepUnknownOpStillCallsBucket verifies that if the catalog
// lookup fails (op_id not found in snapshot) the kernel still rate-limits with
// an empty service-family key rather than panicking — defense in depth, the
// pre-step (resolveVariant) has already validated existence.
func TestTokenBucketStepUnknownOpStillCallsBucket(t *testing.T) {
	tb := &captureBucket{}
	d := &dispatcher{snapshot: &catalog.Catalog{}, tokenBucket: tb}
	_ = d.tokenBucketStep(t.Context(), &Invocation{OpID: "missing.op"}, &ResolvedVariant{OpID: "missing.op"})
	if tb.calls != 1 {
		t.Fatalf("want bucket called once, got %d", tb.calls)
	}
}

// TestTokenBucketStepRateLimitedErrorPropagates asserts that when the wired
// TokenBucket implementation returns a rate-limit sentinel from Wait, the
// dispatch lifecycle propagates it unchanged to the caller — the kernel does
// not swallow or convert this error. This is the end-to-end wiring guarantee
// behind spec §3.1 step 6 and the persistent_bucket.ErrRateLimited contract
// asserted by TestBucketRateLimitedError.
func TestTokenBucketStepRateLimitedErrorPropagates(t *testing.T) {
	sentinel := errors.New("RATE_LIMITED")
	tb := &errBucket{err: sentinel}
	snap := &catalog.Catalog{Ops: []catalog.Op{
		{OpID: "gmail.messages.send", ServiceFamily: "gmail"},
	}}
	d := &dispatcher{snapshot: snap, tokenBucket: tb}

	err := d.tokenBucketStep(t.Context(), &Invocation{OpID: "gmail.messages.send"}, &ResolvedVariant{OpID: "gmail.messages.send"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v; want sentinel %v (Wait error must propagate)", err, sentinel)
	}
}

type errBucket struct {
	err error
}

func (e *errBucket) Wait(_ context.Context, _, _ string) error {
	return e.err
}

type captureBucket struct {
	mu          sync.Mutex
	calls       int
	gotOpID     string
	gotCredsID  string
	familyCalls map[string]int
}

func (c *captureBucket) Wait(_ context.Context, opID, credsID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.gotOpID = opID
	c.gotCredsID = credsID
	if c.familyCalls == nil {
		c.familyCalls = map[string]int{}
	}
	c.familyCalls[credsID]++
	return nil
}

type blockingBucket struct{}

func (b *blockingBucket) Wait(ctx context.Context, _, _ string) error {
	<-ctx.Done()
	return ctx.Err()
}
