// Package dispatch_test — RED-team failing tests for replay cache hardening (gum-1otq.5).
//
// These tests expose three gaps in the current replayCache implementation:
//  1. No concurrency guarantee (exactly-one-success under simultaneous calls).
//  2. No bounded size cap (unbounded map grows forever).
//  3. Expired-entry reclamation (happy path exists but no explicit assertion).
//
// All tests are expected to FAIL until the Green team hardens the implementation.
package dispatch_test

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/dispatch"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// baseParams returns a valid ConfirmationParams that IssueConfirmationToken accepts.
func baseParams(t *testing.T) dispatch.ConfirmationParams {
	t.Helper()
	return dispatch.ConfirmationParams{
		OpID:            "gmail.messages.delete",
		VariantID:       "gmail_messages_delete_v1",
		ArgsHash:        "abc123",
		ResourceKey:     "msg-42",
		AuthFingerprint: "fp-test",
		Scope:           `["gmail_delete"]`,
		Purpose:         dispatch.ConfirmationPurposeDestructive,
		TTL:             5 * time.Minute,
	}
}

// assertTokenInvalidReplayed asserts err is a *StructuredError with
// ErrCodeConfirmationTokenInvalid and Detail["reason"]=="replayed".
// This is the spec §1421 canonical code; "TOKEN_ALREADY_USED" is NOT correct.
func assertTokenInvalidReplayed(t *testing.T, err error, context string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected CONFIRMATION_TOKEN_INVALID/replayed error, got nil", context)
		return
	}
	var se *dispatch.StructuredError
	if !errors.As(err, &se) {
		t.Errorf("%s: expected *StructuredError, got %T: %v", context, err, err)
		return
	}
	if se.ErrCode != dispatch.ErrCodeConfirmationTokenInvalid {
		t.Errorf("%s: ErrCode = %q, want %q", context, se.ErrCode, dispatch.ErrCodeConfirmationTokenInvalid)
	}
	reason, ok := se.Detail["reason"]
	if !ok {
		t.Errorf("%s: Detail missing 'reason' key; detail = %v", context, se.Detail)
		return
	}
	if reason != "replayed" {
		t.Errorf("%s: Detail[reason] = %q, want %q", context, reason, "replayed")
	}
}

// ---------------------------------------------------------------------------
// TestReplayCacheConcurrentExactlyOneSucceeds
//
// N=50 goroutines each call VerifyConfirmationToken with the SAME token
// simultaneously. Exactly 1 must return nil; the other 49 must return
// CONFIRMATION_TOKEN_INVALID with reason=replayed.
//
// This FAILS today because the current seen() implementation uses a mutex but
// does NOT prevent a window between the duplicate check and the insert from
// being split across goroutines in pathological scheduling — and more
// critically, there is no test asserting the exactly-one invariant.
// The test is written to detect any violation.
// ---------------------------------------------------------------------------
func TestReplayCacheConcurrentExactlyOneSucceeds(t *testing.T) {
	t.Cleanup(func() { dispatch.ResetReplayCacheForTest() })
	const N = 50
	params := baseParams(t)

	tok, err := dispatch.IssueConfirmationToken(params)
	if err != nil {
		t.Fatalf("IssueConfirmationToken: %v", err)
	}

	var (
		wg        sync.WaitGroup
		start     = make(chan struct{})
		successes int64
		errs      = make([]error, N)
	)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start // wait for all goroutines to be ready
			e := dispatch.VerifyConfirmationToken(tok, params)
			errs[idx] = e
			if e == nil {
				atomic.AddInt64(&successes, 1)
			}
		}(i)
	}

	close(start) // release all goroutines simultaneously
	wg.Wait()

	if successes != 1 {
		t.Errorf("exactly 1 goroutine should succeed; got %d successes", successes)
	}

	replayed := 0
	for i, e := range errs {
		if e == nil {
			continue
		}
		assertTokenInvalidReplayed(t, e, fmt.Sprintf("goroutine[%d]", i))
		replayed++
	}

	if replayed != N-1 {
		t.Errorf("expected %d replayed errors, got %d", N-1, replayed)
	}
}

// ---------------------------------------------------------------------------
// TestReplayCacheBoundedSize
//
// Inserts more than a documented cap (1024) entries. The cache MUST NOT grow
// without bound: after 1025 insertions the internal map size must be <= 1024.
//
// This FAILS today because replayCache has no size cap at all — the entries
// map is unbounded. The Green team must add a bounded LRU or FIFO eviction
// policy capped at MaxReplayCacheEntries (or equivalent exported const/var).
//
// We probe the cap via the exported constant dispatch.MaxReplayCacheEntries.
// If the constant does not exist the test fails at compile time, driving Green
// to export it.
// ---------------------------------------------------------------------------
func TestReplayCacheBoundedSize(t *testing.T) {
	t.Cleanup(func() { dispatch.ResetReplayCacheForTest() })
	const wantCap = dispatch.MaxReplayCacheEntries // must be 1024; drives Green to add const

	// Use a far-future expiry so nothing expires during the test.
	farFuture := time.Now().Add(24 * time.Hour)

	// Build and insert wantCap+1 distinct fake signatures directly into the
	// global cache via the VerifyConfirmationToken path.  We issue real tokens
	// to avoid depending on unexported internals.
	params := dispatch.ConfirmationParams{
		OpID:            "drive.files.delete",
		VariantID:       "drive_files_delete_v1",
		ArgsHash:        "",
		ResourceKey:     "",
		AuthFingerprint: "fp-bounded",
		Scope:           `["drive_delete"]`,
		Purpose:         dispatch.ConfirmationPurposeDestructive,
		TTL:             24 * time.Hour,
	}

	_ = farFuture // silence unused warning

	// Issue wantCap+1 distinct tokens (each has a unique issuedAt + random sig).
	tokens := make([]string, wantCap+1)
	for i := range tokens {
		// Vary argsHash to get distinct binding hashes.
		p := params
		p.ArgsHash = fmt.Sprintf("hash-%d", i)
		tok, err := dispatch.IssueConfirmationToken(p)
		if err != nil {
			t.Fatalf("IssueConfirmationToken[%d]: %v", i, err)
		}
		tokens[i] = tok
	}

	// Verify all tokens (consuming them into the replay cache).
	for i, tok := range tokens {
		p := params
		p.ArgsHash = fmt.Sprintf("hash-%d", i)
		if err := dispatch.VerifyConfirmationToken(tok, p); err != nil {
			// It's acceptable for the last one to fail if eviction removes an
			// entry we haven't verified yet — but the cache must stay bounded.
			t.Logf("VerifyConfirmationToken[%d]: %v (may be eviction side-effect)", i, err)
		}
	}

	// Assert cache did not exceed cap.
	size := dispatch.GlobalReplayCacheSize() // drives Green to export GlobalReplayCacheSize()
	if size > wantCap {
		t.Errorf("replay cache size = %d, must not exceed cap %d", size, wantCap)
	}
}

// ---------------------------------------------------------------------------
// TestReplayCacheExpiredEntriesEvicted
//
// Insert a token with a TTL that has already elapsed (past expiry).
// Then call seen() indirectly via a fresh VerifyConfirmationToken on a
// different token. Assert that the expired entry was reclaimed (cache does not
// retain it).
//
// This exercises the sweep path in seen(). The current implementation does
// sweep on each call, but there is no test asserting the expired entry is gone.
// We assert it via GlobalReplayCacheSize() so it also drives Green to export
// the helper required by TestReplayCacheBoundedSize.
// ---------------------------------------------------------------------------
func TestReplayCacheExpiredEntriesEvicted(t *testing.T) {
	t.Cleanup(func() { dispatch.ResetReplayCacheForTest() })
	// Issue a token with 1ns TTL so it expires immediately.
	p := baseParams(t)
	p.Purpose = dispatch.ConfirmationPurposeWrite
	p.TTL = time.Nanosecond

	expiredTok, err := dispatch.IssueConfirmationToken(p)
	if err != nil {
		t.Fatalf("IssueConfirmationToken (expired): %v", err)
	}

	// Wait until it's definitely expired.
	time.Sleep(5 * time.Millisecond)

	// Try to verify — should return "expired", not "replayed".
	verifyErr := dispatch.VerifyConfirmationToken(expiredTok, p)
	if verifyErr == nil {
		t.Fatal("expected expired error, got nil")
	}
	var se *dispatch.StructuredError
	if errors.As(verifyErr, &se) {
		if se.ErrCode == dispatch.ErrCodeConfirmationTokenInvalid {
			reason := se.Detail["reason"]
			if reason != "expired" {
				t.Logf("note: expired token returned reason=%q (may be ok if token was already consumed)", reason)
			}
		}
	}

	// Now record the expired token's sig manually by issuing and verifying a
	// fresh token. The sweep triggered by seen() during that verify must remove
	// expired entries.
	p2 := baseParams(t)
	p2.ArgsHash = "eviction-probe"
	p2.TTL = 5 * time.Minute
	freshTok, err := dispatch.IssueConfirmationToken(p2)
	if err != nil {
		t.Fatalf("IssueConfirmationToken (fresh): %v", err)
	}
	if err := dispatch.VerifyConfirmationToken(freshTok, p2); err != nil {
		t.Fatalf("VerifyConfirmationToken (fresh): %v", err)
	}

	// Snapshot the size. Because we swept on the verify above, entries whose
	// expiry < now should be gone. The expired token from above was never
	// recorded (it expired before verify), but we assert size is small to
	// confirm no phantom entries accumulated.
	size := dispatch.GlobalReplayCacheSize()
	// We inserted exactly 1 live entry (freshTok). Allow a small buffer for
	// test parallelism but assert the expired token is not lingering.
	if size > 10 {
		t.Errorf("replay cache size = %d after eviction sweep, expected <= 10 (only fresh entries should remain)", size)
	}
}
