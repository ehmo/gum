// gum-qu0: spec §4.1 contract for gum.poll — progress-token semantics +
// timeout/cancellation lifecycle. The granular handler tests in
// gum_poll_handler_test.go cover each branch independently; these two
// bead-named acceptance tests pin the cross-branch contract under the
// canonical names the spec references.

package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/lro"
)

// TestPollProgressTokenContract — bead-named acceptance for gum-qu0.
//
// Spec §4.1 / §3304-3306: progress notifications MUST be emitted iff the
// caller supplied a _meta.progressToken. Token type (int / string) MUST
// be preserved across the JSON wire. Absent token → zero notifications.
func TestPollProgressTokenContract(t *testing.T) {
	t.Run("with_int_token_emits_progress", func(t *testing.T) {
		srv := newTestServerWithPoller(t, &fakePoller{
			ticks:  []time.Duration{2 * time.Second, 5 * time.Second, 9 * time.Second},
			result: fakePollResult{result: map[string]any{"done": true, "name": "ops/qu0-int"}},
		})
		ss, progressCh, closeFn := sessionPair(t, srv)
		t.Cleanup(closeFn)

		req := makePollRequestWithToken(map[string]any{"operation_name": "ops/qu0-int"}, int64(7))
		req.Session = ss

		res, err := srv.handlePoll(context.Background(), req)
		if err != nil {
			t.Fatalf("handlePoll: %v", err)
		}
		if res == nil || res.IsError {
			t.Fatalf("handlePoll IsError; text=%s", firstText(res))
		}
		notes := drainProgress(progressCh, 500*time.Millisecond, 50*time.Millisecond)
		if len(notes) == 0 {
			t.Fatal("zero progress notifications with progressToken set; want ≥1 (spec §3304)")
		}
		// Token type preserved across wire (int64 or float64 acceptable; never string).
		switch v := notes[0].ProgressToken.(type) {
		case int64:
			if v != 7 {
				t.Errorf("ProgressToken int64=%d; want 7", v)
			}
		case float64:
			if v != 7 {
				t.Errorf("ProgressToken float64=%v; want 7", v)
			}
		default:
			t.Errorf("ProgressToken type=%T; want numeric (int64/float64), got %v", v, v)
		}
	})

	t.Run("with_string_token_preserves_string", func(t *testing.T) {
		srv := newTestServerWithPoller(t, &fakePoller{
			ticks:  []time.Duration{2 * time.Second},
			result: fakePollResult{result: map[string]any{"done": true, "name": "ops/qu0-str"}},
		})
		ss, progressCh, closeFn := sessionPair(t, srv)
		t.Cleanup(closeFn)

		req := makePollRequestWithToken(map[string]any{"operation_name": "ops/qu0-str"}, "qu0-tok")
		req.Session = ss

		if _, err := srv.handlePoll(context.Background(), req); err != nil {
			t.Fatalf("handlePoll: %v", err)
		}
		notes := drainProgress(progressCh, 500*time.Millisecond, 50*time.Millisecond)
		if len(notes) == 0 {
			t.Fatal("zero progress notifications with string token; want ≥1")
		}
		if got := notes[0].ProgressToken; got != "qu0-tok" {
			t.Errorf("ProgressToken=%v (%T); want \"qu0-tok\"", got, got)
		}
	})

	t.Run("no_token_emits_zero_notifications", func(t *testing.T) {
		srv := newTestServerWithPoller(t, &fakePoller{
			ticks:  []time.Duration{2 * time.Second, 5 * time.Second},
			result: fakePollResult{result: map[string]any{"done": true}},
		})
		ss, progressCh, closeFn := sessionPair(t, srv)
		t.Cleanup(closeFn)

		req := makePollRequest(map[string]any{"operation_name": "ops/qu0-none"})
		req.Session = ss

		if _, err := srv.handlePoll(context.Background(), req); err != nil {
			t.Fatalf("handlePoll: %v", err)
		}
		spurious := drainProgress(progressCh, 200*time.Millisecond, 50*time.Millisecond)
		if len(spurious) != 0 {
			t.Errorf("got %d progress notification(s) without token; want 0", len(spurious))
		}
	})
}

// TestPollTimeoutAndCancellation — bead-named acceptance for gum-qu0.
//
// Spec §4.1: the 10-min cap surfaces as a stable LRO_TIMEOUT envelope with a
// resume_handle; ctx cancellation terminates the loop with no goroutine leak
// (goleak.VerifyNone). The default lro.Poller defaults (2s init / 1.5x /
// 60s max / 10-min cap) live in internal/lro — this test pins the handler
// surface for both terminal branches.
func TestPollTimeoutAndCancellation(t *testing.T) {
	defer goleak.VerifyNone(t)

	t.Run("timeout_returns_LRO_TIMEOUT_envelope", func(t *testing.T) {
		srv := newTestServerWithPoller(t, &fakePoller{
			result: fakePollResult{err: &lro.TimeoutError{
				OperationName: "ops/qu0-timeout",
				Elapsed:       10 * time.Minute,
			}},
		})
		req := makePollRequest(map[string]any{"operation_name": "ops/qu0-timeout"})
		res, err := srv.handlePoll(context.Background(), req)
		if err != nil {
			t.Fatalf("handlePoll returned Go error: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(firstText(res)), &m); err != nil {
			t.Fatalf("non-JSON result: %v; text=%s", err, firstText(res))
		}
		if code, _ := m["error_code"].(string); code != "LRO_TIMEOUT" {
			t.Errorf("error_code=%q; want LRO_TIMEOUT (spec §4.1 / §1421)", code)
		}
		if rh, _ := m["resume_handle"].(string); rh != "ops/qu0-timeout" {
			t.Errorf("resume_handle=%q; want \"ops/qu0-timeout\"", rh)
		}
		if sug, _ := m["suggestion"].(string); sug == "" {
			t.Error("LRO_TIMEOUT envelope missing suggestion field")
		}
	})

	t.Run("cancellation_exits_without_goroutine_leak", func(t *testing.T) {
		srv := newTestServerWithPoller(t, &blockingPoller{})
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = srv.handlePoll(ctx, makePollRequest(map[string]any{"operation_name": "ops/qu0-cancel"}))
		}()

		// Give handlePoll a moment to begin blocking, then cancel.
		time.Sleep(20 * time.Millisecond)
		cancel()

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("handlePoll did not exit within 2s after cancellation")
		}
		// goleak.VerifyNone fires from the parent defer.
	})
}
