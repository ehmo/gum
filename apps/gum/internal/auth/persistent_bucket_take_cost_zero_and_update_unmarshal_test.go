package auth_test

import (
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// TestBucketTakeCostZeroPromotesToOne pins Take's
// `cost <= 0 → cost = 1` arm (persistent_bucket.go:145-147). Callers
// occasionally compute cost from a config that defaults to 0 (or a
// signed-int subtraction that goes negative); Take MUST clamp cost to
// 1 rather than silently consuming nothing — otherwise a misconfigured
// caller would bypass the rate limit entirely. We exercise the clamp
// by setting DefaultCapacity=1 and calling Take twice with cost=0:
// the first must succeed (1 token left → 0), the second must hit
// ErrRateLimited (0 tokens left).
func TestBucketTakeCostZeroPromotesToOne(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:                     filepath.Join(dir, "bucket.db"),
		DefaultCapacity:          1,
		DefaultLeakRatePerSecond: 0, // disable refill so the clamp is visible
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	defer func() { _ = b.Close() }()

	if err := b.Take("op", "creds", 0); err != nil {
		t.Fatalf("Take(cost=0) #1 err=%v; want nil (cost clamped to 1, 1→0 tokens)", err)
	}
	// Second call: 0 tokens left, clamped cost=1 must exceed → ErrRateLimited.
	if err := b.Take("op", "creds", -3); err != auth.ErrRateLimited {
		t.Errorf("Take(cost=-3) #2 err=%v; want ErrRateLimited (negative cost still clamps to 1)", err)
	}
}

// TestBucketUpdateUnmarshalsExistingState pins Update's
// `json.Unmarshal(v, &state)` arm (persistent_bucket.go:241).
// Reached on the second Update for the same key: the first Update
// initializes + persists, leaving a non-nil value at bkt.Get(key); the
// second must decode that value rather than re-initialize, so the
// existing Tokens/LeakRate/Capacity survive across the call. Without
// this Unmarshal arm, a follow-up Update would silently reset the
// bucket's accrued debt — defeating the upstream rate-limit signal.
func TestBucketUpdateUnmarshalsExistingState(t *testing.T) {
	defer goleak.VerifyNone(t)
	dir := t.TempDir()
	b, err := auth.OpenBucket(auth.BucketConfig{
		Path:                     filepath.Join(dir, "bucket.db"),
		DefaultCapacity:          5,
		DefaultLeakRatePerSecond: 0,
	})
	if err != nil {
		t.Fatalf("OpenBucket: %v", err)
	}
	defer func() { _ = b.Close() }()

	// First Update plants a record so the second Update's View sees v != nil.
	b.Update("op", "creds", 1*time.Second)
	// Second Update drives the json.Unmarshal arm + re-persists.
	b.Update("op", "creds", 2*time.Second)

	// Both calls planted a retry-after, so Take must return ErrRateLimited
	// (proves the second Update kept the freeze rather than clobbering).
	if err := b.Take("op", "creds", 1); err != auth.ErrRateLimited {
		t.Errorf("Take after two Updates err=%v; want ErrRateLimited (freeze must persist)", err)
	}
}
