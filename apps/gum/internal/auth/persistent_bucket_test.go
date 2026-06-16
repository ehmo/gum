package auth_test

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// ── TestBucketBasicTake ──────────────────────────────────────────────────────

// TestBucketBasicTake verifies that Take succeeds when the bucket has capacity
// and returns ErrRateLimited once the bucket is exhausted.
func TestBucketBasicTake(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:                    filepath.Join(dir, "bucket.db"),
		DefaultCapacity:         3,
		DefaultLeakRatePerSecond: 0, // no replenishment in this test
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	defer func() {
		if err := b.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	opID, credsID := "gmail.users.messages.list", "cred-abc"

	// Should succeed 3 times (capacity=3, cost=1 each).
	for i := 0; i < 3; i++ {
		if err := b.Take(opID, credsID, 1); err != nil {
			t.Fatalf("Take[%d]: unexpected error: %v", i, err)
		}
	}

	// 4th Take should fail with ErrRateLimited.
	err = b.Take(opID, credsID, 1)
	if !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited on 4th Take, got: %v", err)
	}
}

// ── TestBucketLeakRateReplenish ─────────────────────────────────────────────

// TestBucketLeakRateReplenish verifies that after exhausting the bucket, waiting
// for the leak rate to replenish tokens allows a subsequent Take to succeed.
//
// Uses an injected clock so the assertion "ErrRateLimited immediately after
// drain" is deterministic; the previous wall-clock variant was flaky on hosts
// where any pause ≥1ms between Takes let the 1000-tokens/sec rate refill one
// token before the second Take ran (see bead gum-fcij).
func TestBucketLeakRateReplenish(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	clk := newFakeClock(time.Unix(1_700_000_000, 0))
	// Capacity=1, leak rate=1000 tokens/sec → one token replenished per millisecond
	// of *injected* time. Wall-clock pauses no longer affect the assertion.
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:                     filepath.Join(dir, "bucket.db"),
		DefaultCapacity:          1,
		DefaultLeakRatePerSecond: 1000,
		Now:                      clk.Now,
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	defer func() {
		if err := b.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	opID, credsID := "calendar.events.list", "cred-xyz"

	// Drain the single token at t=0.
	if err := b.Take(opID, credsID, 1); err != nil {
		t.Fatalf("first Take: %v", err)
	}

	// Same logical instant: zero refill, must be ErrRateLimited.
	if err := b.Take(opID, credsID, 1); !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited immediately after drain, got: %v", err)
	}

	// Advance the injected clock 5 ms → 5 tokens of theoretical refill, capped
	// at capacity=1 → bucket has exactly 1 token available.
	clk.Advance(5 * time.Millisecond)

	// Should succeed now.
	if err := b.Take(opID, credsID, 1); err != nil {
		t.Errorf("Take after replenishment: expected nil, got: %v", err)
	}
}

// fakeClock is a manually-advanced clock for deterministic Take/Update timing.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// ── TestBucketCrossProcess ───────────────────────────────────────────────────

// TestBucketCrossProcess verifies that bucket state survives Close + reOpen
// (simulating a process restart). If 2 tokens are consumed in "process A",
// "process B" should see only 1 remaining from a capacity of 3.
func TestBucketCrossProcess(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "bucket.db")
	opID, credsID := "drive.files.create", "cred-persist"

	// "Process A": consume 2 tokens from capacity=3.
	func() {
		b, err := auth.OpenBucket(auth.BucketConfig{
			Path:                    dbPath,
			DefaultCapacity:         3,
			DefaultLeakRatePerSecond: 0,
		})
		if err != nil {
			t.Fatalf("Process A OpenBucket: %v", err)
		}
		for i := 0; i < 2; i++ {
			if err := b.Take(opID, credsID, 1); err != nil {
				t.Fatalf("Process A Take[%d]: %v", i, err)
			}
		}
		if err := b.Close(); err != nil {
			t.Fatalf("Process A Close: %v", err)
		}
	}()

	// "Process B": should have 1 token remaining; 2nd Take should fail.
	func() {
		b, err := auth.OpenBucket(auth.BucketConfig{
			Path:                    dbPath,
			DefaultCapacity:         3,
			DefaultLeakRatePerSecond: 0,
		})
		if err != nil {
			t.Fatalf("Process B OpenBucket: %v", err)
		}
		defer func() {
			if err := b.Close(); err != nil {
				t.Errorf("Process B Close: %v", err)
			}
		}()

		// First Take should succeed (1 remaining token).
		if err := b.Take(opID, credsID, 1); err != nil {
			t.Errorf("Process B first Take: expected nil, got: %v", err)
		}
		// Second Take should fail (0 tokens).
		if err := b.Take(opID, credsID, 1); !errors.Is(err, auth.ErrRateLimited) {
			t.Errorf("Process B second Take: expected ErrRateLimited, got: %v", err)
		}
	}()
}

// ── TestBucketRetryAfterHonored ──────────────────────────────────────────────

// TestBucketRetryAfterHonored verifies that Update with a retryAfter duration
// causes Take to return ErrRateLimited until the duration elapses, even if the
// bucket has tokens.
func TestBucketRetryAfterHonored(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:                    filepath.Join(dir, "bucket.db"),
		DefaultCapacity:         100,
		DefaultLeakRatePerSecond: 100,
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	defer func() {
		if err := b.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	opID, credsID := "gmail.users.messages.send", "cred-retry"

	// Simulate an upstream 429 with a 10 ms Retry-After.
	b.Update(opID, credsID, 10*time.Millisecond)

	// Immediately after Update, Take must return ErrRateLimited (bucket frozen).
	if err := b.Take(opID, credsID, 1); !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("expected ErrRateLimited during retry-after freeze, got: %v", err)
	}

	// Wait for retryAfter to elapse.
	time.Sleep(20 * time.Millisecond)

	// Should succeed now.
	if err := b.Take(opID, credsID, 1); err != nil {
		t.Errorf("Take after retry-after elapsed: expected nil, got: %v", err)
	}
}

// ── TestBucketRateLimitedError ────────────────────────────────────────────────

// TestBucketRateLimitedError verifies that ErrRateLimited is a stable sentinel
// and that errors.Is matches it correctly.
func TestBucketRateLimitedError(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := t.TempDir()
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:                    filepath.Join(dir, "bucket.db"),
		DefaultCapacity:         0, // treated as default (100), then override via Update
		DefaultLeakRatePerSecond: 0,
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	defer func() {
		if err := b.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	opID, credsID := "gmail.users.messages.trash", "cred-sentinel"

	// Freeze the bucket with a long Retry-After to guarantee ErrRateLimited.
	b.Update(opID, credsID, 10*time.Second)

	err = b.Take(opID, credsID, 1)
	if err == nil {
		t.Fatal("expected ErrRateLimited, got nil")
	}
	if !errors.Is(err, auth.ErrRateLimited) {
		t.Errorf("expected errors.Is(err, ErrRateLimited) == true, got err: %v", err)
	}
}
