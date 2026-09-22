// Package dispatch — the tee_mode = "failures" step-6 vs step-7 boundary.
//
// docs/expression-profile-dsl.md scopes the "failures" trigger to errors the
// executor raised. Spec §3.1 puts the in-process token bucket at step 6 and
// the executor at step 7, so the SAME error code lands on both sides of the
// boundary: a local bucket refusal and an upstream HTTP 429 both surface as
// RATE_LIMITED. The error code therefore cannot decide whether an artifact is
// owed; only the step can. These tests pin that, because a tee that keyed on
// the code would write a zero-payload artifact for every throttled call the
// kernel never sent, and gum://results/{hash} would resolve a hash to bytes
// that no upstream ever returned.
package dispatch

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// stepBoundaryFixture builds a dispatcher whose step-6 bucket and step-7
// executor fail independently, so one test can move the failure across the
// boundary without changing anything else. bucketErr nil lets step 6 pass;
// execErr nil is never used (a success path writes through step 7c instead).
// The returned counter records executor entries, which is what proves a
// step-6 refusal stopped the call before the upstream leg.
func stepBoundaryFixture(t *testing.T, bucketErr, execErr error) (string, Dispatcher, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	var calls atomic.Int32
	adapter := &funcAdapter{
		execute: func(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			calls.Add(1)
			return nil, execErr
		},
	}
	cfg := DispatcherConfig{Tee: TeeConfig{ProfileDir: dir, RetentionHours: 24}}
	if bucketErr != nil {
		cfg.RateLimiter = tokenBucketFn(func(context.Context, string, string) error { return bucketErr })
	}
	d := NewDispatcherWithConfig(minimalCatalog("stub"), map[string]Adapter{"stub": adapter}, cfg)
	return dir, d, &calls
}

// rateLimitedInvocation is the same invocation for both halves of the pair, so
// the only variable between them is which step fails.
func rateLimitedInvocation(requestID, teeMode string) *Invocation {
	return &Invocation{
		OpID:                   "gum.code",
		Format:                 "json",
		RequestID:              requestID,
		AuthSubjectFingerprint: "fp-test",
		OutputProfile:          &profile.Profile{Recovery: "local_artifact", TeeMode: teeMode},
	}
}

// assertRateLimited fails unless err is the canonical RATE_LIMITED envelope.
// Both halves must reach it, or the pair is not comparing the same failure.
func assertRateLimited(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Dispatch returned nil error; want RATE_LIMITED")
	}
	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("err = %T (%v); want *StructuredError", err, err)
	}
	if se.ErrCode != ErrCodeRateLimited {
		t.Fatalf("ErrCode = %q; want %q", se.ErrCode, ErrCodeRateLimited)
	}
}

func TestTeeFailuresStep6VsStep7(t *testing.T) {
	t.Parallel()

	t.Run("step 6 bucket refusal writes no artifact", func(t *testing.T) {
		t.Parallel()
		dir, d, calls := stepBoundaryFixture(t, ErrRateLimited, errors.New("never reached"))

		_, err := d.Dispatch(context.Background(), rateLimitedInvocation("tee-step6", "failures"))
		assertRateLimited(t, err)

		if got := calls.Load(); got != 0 {
			t.Fatalf("executor ran %d times; want 0 — the bucket refused at step 6", got)
		}
		if got := teeArtifacts(t, dir); len(got) != 0 {
			t.Errorf("tee wrote %d artifacts for a step-6 error; want 0 (no upstream payload exists)", len(got))
		}
	})

	t.Run("step 7 upstream 429 writes one artifact", func(t *testing.T) {
		t.Parallel()
		upstream := `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Quota exceeded"}}`
		dir, d, calls := stepBoundaryFixture(t, nil, &stubUpstreamError{status: 429, body: []byte(upstream)})

		_, err := d.Dispatch(context.Background(), rateLimitedInvocation("tee-step7", "failures"))
		assertRateLimited(t, err)

		if got := calls.Load(); got != 1 {
			t.Fatalf("executor ran %d times; want 1 — the 429 came from step 7", got)
		}
		got := teeArtifacts(t, dir)
		if len(got) != 1 {
			t.Fatalf("tee wrote %d artifacts for a step-7 error; want 1", len(got))
		}
		if body := readArtifact(t, got[0]); body != upstream {
			t.Errorf("artifact body = %q; want the verbatim upstream body %q", body, upstream)
		}
	})

	t.Run("the boundary holds when the bucket refusal carries no sentinel", func(t *testing.T) {
		t.Parallel()
		// A bucket that returns its own error rather than ErrRateLimited still
		// fails at step 6. mapRateLimited leaves such an error alone, so the
		// envelope is not RATE_LIMITED — but the tee decision is unchanged,
		// which is the point: the step decides, not the code.
		dir, d, calls := stepBoundaryFixture(t, errors.New("bucket closed for maintenance"), nil)

		_, err := d.Dispatch(context.Background(), rateLimitedInvocation("tee-step6-plain", "failures"))
		if err == nil || !strings.Contains(err.Error(), "bucket closed") {
			t.Fatalf("err = %v; want the bucket error", err)
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("executor ran %d times; want 0", got)
		}
		if got := teeArtifacts(t, dir); len(got) != 0 {
			t.Errorf("tee wrote %d artifacts; want 0 for any step-6 error", len(got))
		}
	})

	t.Run("tee_mode always does not lower the boundary to step 6", func(t *testing.T) {
		t.Parallel()
		// "always" is a superset of "failures" over the failures the mode can
		// see. It does not extend the tee backwards over steps 1-6, because
		// there is still no upstream payload to write.
		dir, d, _ := stepBoundaryFixture(t, ErrRateLimited, errors.New("never reached"))

		_, err := d.Dispatch(context.Background(), rateLimitedInvocation("tee-step6-always", "always"))
		assertRateLimited(t, err)

		if got := teeArtifacts(t, dir); len(got) != 0 {
			t.Errorf("tee wrote %d artifacts under tee_mode=always for a step-6 error; want 0", len(got))
		}
	})

	t.Run("tee_mode off suppresses the step 7 artifact", func(t *testing.T) {
		t.Parallel()
		// The mirror of the previous case: the step decides whether an
		// artifact is owed, the mode decides whether it is written. Both gates
		// must hold, or one of the two halves above would pass for the wrong
		// reason.
		dir, d, _ := stepBoundaryFixture(t, nil, &stubUpstreamError{status: 429, body: []byte(`{"error":"slow down"}`)})

		_, err := d.Dispatch(context.Background(), rateLimitedInvocation("tee-step7-off", "off"))
		assertRateLimited(t, err)

		if got := teeArtifacts(t, dir); len(got) != 0 {
			t.Errorf("tee wrote %d artifacts under tee_mode=off; want 0", len(got))
		}
	})
}
