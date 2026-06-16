package adapters

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/dispatch"
)

// whiteboxMockDispatcher is the in-package dispatch.Dispatcher used by
// internal tests that bypass Risor.
type whiteboxMockDispatcher struct {
	fn func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error)
}

func (m *whiteboxMockDispatcher) Dispatch(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return m.fn(ctx, inv)
}

// whiteboxFamilyDispatcher implements both dispatch.Dispatcher and
// dispatch.ServiceFamilyResolver so gum_parallel's 429 isolation can map
// op_ids to service families inside the test without spinning up a real
// catalog snapshot.
type whiteboxFamilyDispatcher struct {
	fn       func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error)
	families map[string]string
}

func (m *whiteboxFamilyDispatcher) Dispatch(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return m.fn(ctx, inv)
}

func (m *whiteboxFamilyDispatcher) ServiceFamily(opID string) string {
	return m.families[opID]
}

// TestGumParallelCancellationProducesCANCELLEDWhitebox calls buildParallelFn
// directly so the Risor VM is not in the loop. Spec §6.3 lines 1007-1016 /
// §1421: workers blocked on the upstream call receive a cancelled context
// and the envelope's per-element entries carry the canonical
// {error_code: "CANCELLED", cancelled: true} shape.
func TestGumParallelCancellationProducesCANCELLEDWhitebox(t *testing.T) {
	startCh := make(chan struct{}, 32)
	mock := &whiteboxMockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		select {
		case startCh <- struct{}{}:
		default:
		}
		// Block until cancelled.
		<-ctx.Done()
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeCancelled, "context cancelled")
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel once 6 workers have started.
	go func() {
		for i := 0; i < 6; i++ {
			<-startCh
		}
		cancel()
	}()

	fn := buildParallelFn(ctx, mock, false, false)
	entries := make([]any, 12)
	for i := range entries {
		entries[i] = map[string]any{"op": "op.x"}
	}
	got, err := fn(entries)
	if err != nil {
		t.Fatalf("gum_parallel returned error: %v", err)
	}
	env, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map envelope, got %T", got)
	}

	results, ok := env["results"].([]any)
	if !ok {
		t.Fatalf("envelope.results is not []any: %T", env["results"])
	}
	if len(results) != 12 {
		t.Fatalf("len(results) = %d; want 12", len(results))
	}

	cancelledCount := 0
	for i, r := range results {
		rm, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("results[%d] is not map: %T", i, r)
		}
		errObj, _ := rm["error"].(map[string]any)
		if errObj == nil {
			continue
		}
		if errObj["error_code"] == "CANCELLED" {
			cancelledCount++
			if cancelled, _ := errObj["cancelled"].(bool); !cancelled {
				t.Errorf("results[%d].error.cancelled = %v; want true", i, errObj["cancelled"])
			}
		}
	}
	if cancelledCount == 0 {
		t.Errorf("no CANCELLED elements; results=%v", results)
	}
}

// TestGumParallelCancelBeforeAnyDispatchAllCancelled verifies that a context
// cancelled BEFORE buildParallelFn runs produces CANCELLED envelopes for
// every element (none of which ever reached a worker).
func TestGumParallelCancelBeforeAnyDispatchAllCancelled(t *testing.T) {
	mock := &whiteboxMockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		t.Errorf("dispatcher should not be invoked when ctx is pre-cancelled")
		return nil, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	fn := buildParallelFn(ctx, mock, false, false)
	entries := []any{
		map[string]any{"op": "op.a"},
		map[string]any{"op": "op.b"},
		map[string]any{"op": "op.c"},
	}
	got, err := fn(entries)
	if err != nil {
		t.Fatalf("gum_parallel returned error: %v", err)
	}
	env := got.(map[string]any)
	results := env["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("len(results) = %d; want 3", len(results))
	}
	for i, r := range results {
		rm := r.(map[string]any)
		errObj, _ := rm["error"].(map[string]any)
		if errObj == nil {
			t.Errorf("results[%d] missing error envelope on pre-cancel: %v", i, rm)
			continue
		}
		if errObj["error_code"] != "CANCELLED" {
			t.Errorf("results[%d].error.error_code = %q; want CANCELLED", i, errObj["error_code"])
		}
		if cancelled, _ := errObj["cancelled"].(bool); !cancelled {
			t.Errorf("results[%d].error.cancelled = %v; want true", i, errObj["cancelled"])
		}
	}
}

// TestGumParallelOuterEnvelopeContract verifies the §9.0.1 outer envelope
// fields directly (white-box). Specifically:
//   - format = "parallel_results"
//   - batch_id is non-empty 8-char hex
//   - _expression.op_id = "gum_parallel"
//   - _expression.variant_id = nil
func TestGumParallelOuterEnvelopeContract(t *testing.T) {
	mock := &whiteboxMockDispatcher{fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{Format: "json", StructuredContent: map[string]any{"ok": true}}, nil
	}}
	fn := buildParallelFn(context.Background(), mock, false, false)
	got, err := fn([]any{map[string]any{"op": "op.a"}, map[string]any{"op": "op.b"}})
	if err != nil {
		t.Fatal(err)
	}
	env := got.(map[string]any)
	if env["format"] != "parallel_results" {
		t.Errorf("format = %v; want parallel_results", env["format"])
	}
	bid, ok := env["batch_id"].(string)
	if !ok || len(bid) != 8 {
		t.Errorf("batch_id = %v (len %d); want 8-char hex", env["batch_id"], len(bid))
	}
	outerExpr, ok := env["_expression"].(map[string]any)
	if !ok {
		t.Fatalf("envelope._expression is not map: %T", env["_expression"])
	}
	if outerExpr["op_id"] != "gum_parallel" {
		t.Errorf("outer _expression.op_id = %v; want gum_parallel", outerExpr["op_id"])
	}
	if outerExpr["variant_id"] != nil {
		t.Errorf("outer _expression.variant_id = %v; want nil (spec §9.0.1)", outerExpr["variant_id"])
	}
}

// TestGumParallel429ServiceFamilyIsolation is the gum-e9d acceptance test.
// Spec §6.3 line 1171: a 429 on a Gmail op pauses only the gmail family for
// retry_after_ms; workers in the drive family continue uninterrupted.
//
// Setup: 8 elements (4 gmail.*, 4 drive.*). Gmail returns RATE_LIMITED with
// retry_after_ms=300. Drive returns OK after a 5ms simulated upstream call.
// Worker pool is 8, so every element starts immediately.
//
// Assertions:
//  1. All 4 drive elements complete within ~150ms (well under the 300ms
//     gmail pause) — they MUST NOT be stalled by the gmail-family pause.
//  2. The 4 gmail elements all carry error_code=RATE_LIMITED. The first
//     gmail call lands before the pause is installed (returns immediately);
//     subsequent gmail dispatches are gated. Crucially, no drive call ever
//     waits for the gate.
//  3. Worker concurrency is not artificially serialised by the gate — drive
//     workers continue dispatching while gmail workers are paused.
func TestGumParallel429ServiceFamilyIsolation(t *testing.T) {
	const retryAfterMs = 300
	const driveLatency = 5 * time.Millisecond
	const familyPause = time.Duration(retryAfterMs) * time.Millisecond

	var gmailCalls, driveCalls atomic.Int32
	driveCompletedBy := make(chan time.Time, 4)

	mock := &whiteboxFamilyDispatcher{
		families: map[string]string{
			"gmail.list":  "gmail",
			"drive.files": "drive",
		},
		fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
			if strings.HasPrefix(inv.OpID, "gmail.") {
				gmailCalls.Add(1)
				return nil, dispatch.NewStructuredError(dispatch.ErrCodeRateLimited, "upstream rate-limited (HTTP 429)").
					WithRetryable(true).
					WithDetail("retry_after_ms", int64(retryAfterMs))
			}
			// drive.* — simulate a real upstream call.
			select {
			case <-time.After(driveLatency):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			driveCalls.Add(1)
			driveCompletedBy <- time.Now()
			return &dispatch.ShapedResponse{Format: "json", StructuredContent: map[string]any{"ok": true}}, nil
		},
	}

	entries := []any{
		map[string]any{"op": "gmail.list"},
		map[string]any{"op": "drive.files"},
		map[string]any{"op": "gmail.list"},
		map[string]any{"op": "drive.files"},
		map[string]any{"op": "gmail.list"},
		map[string]any{"op": "drive.files"},
		map[string]any{"op": "gmail.list"},
		map[string]any{"op": "drive.files"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	fn := buildParallelFn(ctx, mock, false, false)
	start := time.Now()
	got, err := fn(entries)
	if err != nil {
		t.Fatalf("gum_parallel: %v", err)
	}

	env := got.(map[string]any)
	results := env["results"].([]any)
	if len(results) != 8 {
		t.Fatalf("len(results) = %d; want 8", len(results))
	}

	close(driveCompletedBy)
	var lastDriveCompletion time.Time
	for ts := range driveCompletedBy {
		if ts.After(lastDriveCompletion) {
			lastDriveCompletion = ts
		}
	}

	// (1) All four drive elements completed well before the 300ms gmail
	// pause would expire — proving they were NOT held by the gate.
	driveElapsed := lastDriveCompletion.Sub(start)
	if driveElapsed >= familyPause {
		t.Errorf("drive workers were stalled: lastDriveCompletion = %v after start; want < %v (the gmail family-pause window) — spec §6.3 line 1171",
			driveElapsed, familyPause)
	}
	if got := driveCalls.Load(); got != 4 {
		t.Errorf("drive dispatch count = %d; want 4 (all drive workers should have been free to run)", got)
	}

	// (2) Per-element shape: every gmail result carries RATE_LIMITED;
	// every drive result is a success envelope with format=json.
	gmailRL, driveOK := 0, 0
	for i, r := range results {
		rm := r.(map[string]any)
		expr, _ := rm["_expression"].(map[string]any)
		opID, _ := expr["op_id"].(string)
		if strings.HasPrefix(opID, "gmail.") {
			errObj, ok := rm["error"].(map[string]any)
			if !ok {
				t.Errorf("results[%d] gmail: missing error envelope: %v", i, rm)
				continue
			}
			if errObj["error_code"] != string(dispatch.ErrCodeRateLimited) {
				t.Errorf("results[%d] gmail.error.error_code = %v; want RATE_LIMITED", i, errObj["error_code"])
			}
			gmailRL++
		} else if strings.HasPrefix(opID, "drive.") {
			if _, hasErr := rm["error"]; hasErr {
				t.Errorf("results[%d] drive: unexpected error envelope: %v", i, rm["error"])
				continue
			}
			if rm["format"] != "json" {
				t.Errorf("results[%d] drive: format = %v; want json", i, rm["format"])
			}
			driveOK++
		}
	}
	if gmailRL != 4 {
		t.Errorf("gmail RATE_LIMITED count = %d; want 4", gmailRL)
	}
	if driveOK != 4 {
		t.Errorf("drive success count = %d; want 4", driveOK)
	}
}

// TestGumParallel429SameFamilyIsPaused is the negative-control companion to
// TestGumParallel429ServiceFamilyIsolation: when ALL ops share the same
// family and one of them 429s, subsequent dispatches in that family MUST
// honour the retry_after_ms pause window. This proves the gate engages at
// all (without it, the isolation test could falsely pass).
//
// The family pause is recorded AFTER a dispatch returns RATE_LIMITED, so it
// only gates dispatches that workers pick up later. With nWorkers >= len(entries)
// every entry is in the first cohort and there are no "later" dispatches to
// gate — the gate has nothing to do. To exercise the gate, entries MUST exceed
// parallelMaxWorkers AND the success path MUST take long enough that the
// 429-returning worker records the pause before any cohort-2 job is claimed.
func TestGumParallel429SameFamilyIsPaused(t *testing.T) {
	const retryAfterMs = 200
	familyPause := time.Duration(retryAfterMs) * time.Millisecond
	// More than parallelMaxWorkers (8) so a second cohort of dispatches exists.
	const numEntries = 16
	// Long enough that cohort-1 workers don't finish and grab cohort-2 jobs
	// before the 429-returning worker calls gate.pause.
	const successLatency = 30 * time.Millisecond

	var calls atomic.Int32
	var firstCallAt time.Time
	var lastCallAt time.Time
	var mu sync.Mutex

	mock := &whiteboxFamilyDispatcher{
		families: map[string]string{"gmail.list": "gmail"},
		fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
			n := calls.Add(1)
			now := time.Now()
			mu.Lock()
			if n == 1 {
				firstCallAt = now
			}
			if now.After(lastCallAt) {
				lastCallAt = now
			}
			mu.Unlock()
			if n == 1 {
				return nil, dispatch.NewStructuredError(dispatch.ErrCodeRateLimited, "upstream rate-limited (HTTP 429)").
					WithRetryable(true).
					WithDetail("retry_after_ms", int64(retryAfterMs))
			}
			select {
			case <-time.After(successLatency):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &dispatch.ShapedResponse{Format: "json", StructuredContent: map[string]any{"ok": true}}, nil
		},
	}

	entries := make([]any, numEntries)
	for i := range entries {
		entries[i] = map[string]any{"op": "gmail.list"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fn := buildParallelFn(ctx, mock, false, false)
	got, err := fn(entries)
	if err != nil {
		t.Fatalf("gum_parallel: %v", err)
	}
	env := got.(map[string]any)
	results := env["results"].([]any)
	if len(results) != numEntries {
		t.Fatalf("len(results) = %d; want %d", len(results), numEntries)
	}

	mu.Lock()
	gap := lastCallAt.Sub(firstCallAt)
	mu.Unlock()
	minGap := familyPause * 9 / 10
	if gap < minGap {
		t.Errorf("same-family pause not honoured: lastCall - firstCall = %v; want >= %v (retry_after_ms=%d) — spec §6.3 line 1171",
			gap, minGap, retryAfterMs)
	}
}
