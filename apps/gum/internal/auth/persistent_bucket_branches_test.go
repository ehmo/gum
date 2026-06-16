package auth_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// monoClock is a manually-advanced clock — duplicated here to avoid
// reaching into persistent_bucket_test.go's unexported fakeClock.
type monoClock struct{ now time.Time }

func (c *monoClock) Now() time.Time      { return c.now }
func (c *monoClock) Set(t time.Time)     { c.now = t }
func (c *monoClock) Add(d time.Duration) { c.now = c.now.Add(d) }

// TestBucketUpdateZeroRetryAfterIsNoOp pins PersistentBucket.Update's
// `retryAfter <= 0 → return` early-out arm (persistent_bucket.go:221-223).
// Upstream signals occasionally carry a zero/negative Retry-After (e.g.
// header parse fallback). The call MUST be a no-op rather than mutating
// state with a stale freeze time.
func TestBucketUpdateZeroRetryAfterIsNoOp(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:            filepath.Join(dir, "bucket.db"),
		DefaultCapacity: 3,
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	defer func() { _ = b.Close() }()

	// Update with zero retryAfter — early return, no freeze should land.
	b.Update("op", "creds", 0)
	// Update with negative retryAfter — same path.
	b.Update("op", "creds", -5*time.Second)

	// Bucket must still be takeable (no retry-after freeze applied).
	if err := b.Take("op", "creds", 1); err != nil {
		t.Errorf("Take after zero-Update = %v; want nil (no freeze planted)", err)
	}
}

// TestBucketTakeAfterCloseWrapsReadErr pins Take's
// `b.db.View err → return fmt.Errorf("auth: read bucket state: %w", ...)`
// arm (persistent_bucket.go:167-169). Once Close has run, bbolt's View
// returns "database not open"; Take MUST wrap it so callers see the
// "read bucket state:" prefix instead of a bare bbolt error.
func TestBucketTakeAfterCloseWrapsReadErr(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:            filepath.Join(dir, "bucket.db"),
		DefaultCapacity: 3,
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = b.Take("op", "creds", 1)
	if err == nil {
		t.Fatal("Take(closed-db) err=nil; want read-state wrap")
	}
	if !strings.Contains(err.Error(), "read bucket state:") {
		t.Errorf("err=%q; want 'read bucket state:' prefix", err.Error())
	}
}

// TestBucketTakeClampsNegativeElapsed pins Take's
// `elapsedSec < 0 → elapsedSec = 0` arm (persistent_bucket.go:192-194).
// A backward clock jump (NTP correction, suspend/resume) would otherwise
// produce a negative leak that subtracts tokens. The clamp protects the
// bucket from going below its seeded value.
func TestBucketTakeClampsNegativeElapsed(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	clk := &monoClock{now: time.Unix(1_700_000_000, 0)}
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:                     filepath.Join(dir, "bucket.db"),
		DefaultCapacity:          5,
		DefaultLeakRatePerSecond: 1.0,
		Now:                      clk.Now,
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	defer func() { _ = b.Close() }()

	// Seed: first Take stamps LastRefillUnixNano at clk.now.
	if err := b.Take("op", "creds", 1); err != nil {
		t.Fatalf("Take seed: %v", err)
	}

	// Backward jump — second Take computes elapsed<0, hits clamp.
	clk.Set(time.Unix(1_700_000_000-60, 0)) // 60s in the past
	if err := b.Take("op", "creds", 1); err != nil {
		t.Errorf("Take after clock-jump backward = %v; want nil (clamp prevents over-debit)", err)
	}
}
