package adapters

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/dispatch"
	"go.uber.org/goleak"
)

// Batch-accounting and cancellation gates for gum_parallel (beads gum-7oap,
// gum-z8u9; docs/test-matrix.md).
//
// The §12.3 ledger describes one gum_parallel call as an outer sentinel entry
// plus one inner entry per element, joined by a shared batch_id. Nothing joins
// them unless the id exists before the first dispatch and rides on every
// Invocation, so these tests read the ids off the invocations the batch made,
// not off the envelope alone.

// recordingBatchDispatcher implements dispatch.Dispatcher and
// dispatch.ParallelBatchRecorder, keeping every invocation it dispatched and
// every batch it was asked to record.
type recordingBatchDispatcher struct {
	fn func(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error)

	mu      sync.Mutex
	invs    []dispatch.Invocation
	batches []dispatch.ParallelBatch
}

func (m *recordingBatchDispatcher) Dispatch(ctx context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	m.mu.Lock()
	m.invs = append(m.invs, *inv)
	m.mu.Unlock()
	return m.fn(ctx, inv)
}

func (m *recordingBatchDispatcher) RecordParallelBatch(b dispatch.ParallelBatch) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batches = append(m.batches, b)
}

func (m *recordingBatchDispatcher) recorded() []dispatch.ParallelBatch {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]dispatch.ParallelBatch(nil), m.batches...)
}

func (m *recordingBatchDispatcher) invocations() []dispatch.Invocation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]dispatch.Invocation(nil), m.invs...)
}

func okDispatcher() *recordingBatchDispatcher {
	return &recordingBatchDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		return &dispatch.ShapedResponse{Body: []byte(`{"ok":true}`), Format: "json"}, nil
	}}
}

// oneBatch returns the single batch the dispatcher was asked to record.
func oneBatch(t *testing.T, d *recordingBatchDispatcher) dispatch.ParallelBatch {
	t.Helper()

	got := d.recorded()
	if len(got) != 1 {
		t.Fatalf("RecordParallelBatch calls = %d; want exactly 1 per gum_parallel call", len(got))
	}
	return got[0]
}

func TestGumParallelInnerInvocationsCarryBatchLinkage(t *testing.T) {
	disp := okDispatcher()

	env := runParallelBatch(context.Background(), disp, []parallelElement{
		{OpID: "gmail.users.messages.list", Args: map[string]any{"q": "a"}},
		{OpID: "calendar.events.list", Args: map[string]any{"q": "b"}},
		{OpID: "drive.files.list", Args: map[string]any{"q": "c"}},
	}, false, false, parallelBudget{})

	batchID, _ := env["batch_id"].(string)
	if len(batchID) != 8 {
		t.Fatalf("envelope batch_id = %q; want an 8-char hex id", env["batch_id"])
	}

	invs := disp.invocations()
	if len(invs) != 3 {
		t.Fatalf("dispatched %d elements; want 3", len(invs))
	}

	seen := map[int]bool{}
	for _, inv := range invs {
		if inv.BatchID != batchID {
			t.Errorf("op %s dispatched with BatchID %q; want the envelope's %q, or its ledger entry joins no batch",
				inv.OpID, inv.BatchID, batchID)
		}
		if seen[inv.BatchIndex] {
			t.Errorf("BatchIndex %d dispatched twice; §12.3 indexes are 0..N-1 and unique", inv.BatchIndex)
		}
		seen[inv.BatchIndex] = true
	}
	for i := 0; i < 3; i++ {
		if !seen[i] {
			t.Errorf("no element dispatched with BatchIndex %d", i)
		}
	}
}

func TestGumParallelRecordsOuterBatchEntry(t *testing.T) {
	disp := okDispatcher()
	elements := []parallelElement{
		{OpID: "gmail.users.messages.list", Args: map[string]any{"q": "a"}},
		{OpID: "calendar.events.list", Args: map[string]any{"q": "b"}},
	}

	env := runParallelBatch(context.Background(), disp, elements, false, false, parallelBudget{})
	batch := oneBatch(t, disp)

	if batch.BatchID != env["batch_id"] {
		t.Errorf("recorded BatchID = %q; envelope batch_id = %v", batch.BatchID, env["batch_id"])
	}
	if len(batch.Elements) != len(elements) {
		t.Errorf("recorded %d elements; want %d, which is the outer entry's element_count",
			len(batch.Elements), len(elements))
	}
	for i, el := range batch.Elements {
		if el.OpID != elements[i].OpID {
			t.Errorf("element %d op_id = %q; want %q", i, el.OpID, elements[i].OpID)
		}
		if el.Cancelled {
			t.Errorf("element %d recorded as cancelled; the batch ran to completion", i)
		}
	}
	if batch.Cancelled {
		t.Error("outer entry marked cancelled; the batch ran to completion")
	}
	if len(batch.Args) == 0 {
		t.Error("recorded batch carries no Args, so the outer entry's args_hash would digest nothing")
	}
	if batch.Envelope["batch_id"] != env["batch_id"] {
		t.Error("recorded Envelope is not the envelope the caller received, so response_tokens prices the wrong bytes")
	}
}

func TestGumParallelEmptyBatchIsStillRecorded(t *testing.T) {
	disp := okDispatcher()

	runParallelBatch(context.Background(), disp, nil, false, false, parallelBudget{})

	batch := oneBatch(t, disp)
	if len(batch.Elements) != 0 {
		t.Errorf("recorded %d elements for an empty batch; want 0", len(batch.Elements))
	}
	if batch.BatchID == "" {
		t.Error("empty batch recorded with no BatchID")
	}
}

func TestGumParallelCancellationMarksBatchAndElements(t *testing.T) {
	disp := &recordingBatchDispatcher{fn: func(context.Context, *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		t.Error("dispatcher ran despite a pre-cancelled context")
		return nil, nil
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runParallelBatch(ctx, disp, []parallelElement{
		{OpID: "op.a"},
		{OpID: "op.b"},
	}, false, false, parallelBudget{})

	batch := oneBatch(t, disp)
	if !batch.Cancelled {
		t.Error("outer entry not marked cancelled; §12.3 sets cancelled=true when the outer context was cancelled")
	}
	for i, el := range batch.Elements {
		if !el.Cancelled {
			t.Errorf("element %d not marked cancelled; the matrix requires a per-element cancelled:true ledger entry", i)
		}
	}
}

// TestGumParallelCancellationPropagatesWithin200ms is the row-151 SLA: a
// cancelled outer context has to reach workers already blocked on an upstream
// call, not just stop new ones from starting.
func TestGumParallelCancellationPropagatesWithin200ms(t *testing.T) {
	const elements = 12

	started := make(chan struct{}, elements)
	disp := &recordingBatchDispatcher{fn: func(ctx context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeCancelled, "context cancelled")
	}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	els := make([]parallelElement, elements)
	for i := range els {
		els[i] = parallelElement{OpID: "op.x"}
	}

	done := make(chan map[string]any, 1)
	go func() {
		done <- runParallelBatch(ctx, disp, els, false, false, parallelBudget{})
	}()

	// Wait for the pool to fill so the cancel lands on in-flight calls.
	for i := 0; i < parallelMaxWorkers; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d workers started", i, parallelMaxWorkers)
		}
	}

	cancelledAt := time.Now()
	cancel()

	select {
	case env := <-done:
		if elapsed := time.Since(cancelledAt); elapsed > 200*time.Millisecond {
			t.Errorf("batch returned %v after cancel; the matrix caps propagation at 200ms", elapsed)
		}
		results, _ := env["results"].([]any)
		if len(results) != elements {
			t.Fatalf("len(results) = %d; want %d", len(results), elements)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("batch never returned after cancel")
	}
}

// TestGumParallelLeaksNoGoroutines is the other half of the batch-ledger
// row: the worker
// pool and the feeder goroutine both have to be joined before the batch
// returns, on the cancelled path as well as the normal one.
func TestGumParallelLeaksNoGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t)

	els := make([]parallelElement, 20)
	for i := range els {
		els[i] = parallelElement{OpID: "op.x"}
	}

	runParallelBatch(context.Background(), okDispatcher(), els, false, false, parallelBudget{})

	blocking := &recordingBatchDispatcher{fn: func(ctx context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
		<-ctx.Done()
		return nil, dispatch.NewStructuredError(dispatch.ErrCodeCancelled, "context cancelled")
	}}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	runParallelBatch(ctx, blocking, els, false, false, parallelBudget{})
	cancel()
}
