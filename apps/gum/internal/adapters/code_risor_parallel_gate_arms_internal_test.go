package adapters

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/dispatch"
)

// TestRunParallelBatchWithNoElements pins the empty-batch short circuit. The
// Risor entry point rejects an empty list before this point, so the guard is
// what keeps a direct caller from starting eight workers with no work.
func TestRunParallelBatchWithNoElements(t *testing.T) {
	t.Parallel()
	env := runParallelBatch(context.Background(), &whiteboxMockDispatcher{}, nil, false, false, parallelBudget{})
	results, ok := env["results"].([]any)
	if !ok {
		t.Fatalf("results type %T; want []any", env["results"])
	}
	if len(results) != 0 {
		t.Errorf("results = %d; want 0", len(results))
	}
}

// TestFamilyGateWaitReturnsOnCancellation pins the ctx.Done arm of the pause
// select. A cancelled batch must not hold a worker for the full retry_after
// window.
func TestFamilyGateWaitReturnsOnCancellation(t *testing.T) {
	t.Parallel()
	g := newFamilyGate()
	g.pause("gmail", time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if !g.wait(ctx, "gmail", 0) {
		t.Fatal("wait returned false; a paused family must report the honoured pause")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("wait took %v; a cancelled context must return at once", elapsed)
	}
}

// TestFamilyGateWaitStopsStaggerOnCancellation pins the ctx.Done arm of the
// stagger select. The pause itself elapses, then the deadline fires during
// the worker's thundering-herd offset.
func TestFamilyGateWaitStopsStaggerOnCancellation(t *testing.T) {
	t.Parallel()
	g := newFamilyGate()
	g.pause("gmail", 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	// workerIdx 4 asks for a 4*parallel429StaggerStep offset, well past the
	// context deadline.
	if !g.wait(ctx, "gmail", 4) {
		t.Fatal("wait returned false for a paused family")
	}
	if elapsed := time.Since(start); elapsed >= 4*parallel429StaggerStep {
		t.Errorf("wait took %v; the deadline must cut the stagger short of %v",
			elapsed, 4*parallel429StaggerStep)
	}
}

// TestParallelWorkerCancelsAnElementPausedPastTheDeadline pins the
// gate-then-cancelled arm inside the worker loop. One element's 429 pauses
// the whole family for longer than the batch has left, so the element a freed
// worker picks up next must come back CANCELLED and never reach the kernel.
func TestParallelWorkerCancelsAnElementPausedPastTheDeadline(t *testing.T) {
	t.Parallel()

	var lateCalls atomic.Int32
	mock := &whiteboxFamilyDispatcher{
		families: map[string]string{
			"gmail.limit": "gmail",
			"gmail.slow":  "gmail",
			"gmail.late":  "gmail",
		},
		fn: func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
			switch inv.OpID {
			case "gmail.limit":
				return nil, dispatch.NewStructuredError(dispatch.ErrCodeRateLimited, "upstream rate-limited (HTTP 429)").
					WithRetryable(true).
					WithDetail("retry_after_ms", int64(60000))
			case "gmail.late":
				lateCalls.Add(1)
				return &dispatch.ShapedResponse{Format: "json"}, nil
			}
			select {
			case <-time.After(30 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &dispatch.ShapedResponse{Format: "json"}, nil
		},
	}

	// Eight elements saturate the worker pool; the ninth waits for a free
	// worker, which is the one whose 429 just paused the family.
	elements := []parallelElement{{OpID: "gmail.limit"}}
	for i := 0; i < parallelMaxWorkers-1; i++ {
		elements = append(elements, parallelElement{OpID: "gmail.slow"})
	}
	elements = append(elements, parallelElement{OpID: "gmail.late"})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	env := runParallelBatch(ctx, mock, elements, false, false, parallelBudget{})
	results := env["results"].([]any)
	last, ok := results[len(results)-1].(map[string]any)
	if !ok {
		t.Fatalf("last result type %T; want map[string]any", results[len(results)-1])
	}
	errItem, ok := last["error"].(map[string]any)
	if !ok {
		t.Fatalf("last element carries no error map: %v", last)
	}
	if code, _ := errItem["error_code"].(string); code != "CANCELLED" {
		t.Fatalf("last element error_code = %v; want CANCELLED (item %v)", code, last)
	}
	if n := lateCalls.Load(); n != 0 {
		t.Errorf("gmail.late dispatched %d time(s); the family pause must hold it back", n)
	}
	if got, _ := errItem["op_id"].(string); !strings.Contains(got, "gmail.late") {
		t.Errorf("cancelled item op_id = %q; want gmail.late", got)
	}
}

// TestDispatchOneSkipsAnAlreadyCancelledElement pins the pre-dispatch
// cancellation check. A worker that picks up an element after the batch
// deadline passed must emit the CANCELLED item without touching the kernel.
func TestDispatchOneSkipsAnAlreadyCancelledElement(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	disp := &whiteboxMockDispatcher{fn: func(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		calls.Add(1)
		return &dispatch.ShapedResponse{Body: []byte(`{}`)}, nil
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	item := dispatchOne(ctx, disp, "deadbeef", 3, parallelElement{OpID: "gmail.list"}, false, false)
	errItem, ok := item["error"].(map[string]any)
	if !ok {
		t.Fatalf("item = %#v; want a nested error map", item)
	}
	if errItem["error_code"] != string(dispatch.ErrCodeCancelled) {
		t.Errorf("error_code = %v; want CANCELLED", errItem["error_code"])
	}
	if item["_idx"] != 3 {
		t.Errorf("_idx = %v; want 3", item["_idx"])
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("dispatch calls = %d; want 0", n)
	}
}
