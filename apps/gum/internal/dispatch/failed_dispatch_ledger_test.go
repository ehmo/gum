package dispatch

import (
	"context"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/gain"
)

// Spec §12.3: "every completed dispatch appends one authoritative local
// gain-ledger entry", and an entry carries an "optional error_code for failed
// dispatches". Step 9 ran only on the success path, so a window's baseline
// omitted every failed call and a profile that failed half its calls read the
// same as one that failed none.
//
// A failed call has no upstream body and no shaped body, so raw_tokens and
// shaped_tokens are 0. That keeps it inert in the release subtotal, which
// admits only baseline_method="fixture_replay" rows, while still counting as
// one call in the window.

// failingAdapter returns err for every invocation.
func failingAdapter(err error) map[string]Adapter {
	return map[string]Adapter{"noop": AdapterFunc(func(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error) {
		return nil, err
	})}
}

func failureLedgerDispatcher(t *testing.T, cat *catalog.Catalog, adapters map[string]Adapter) (Dispatcher, *captureLedger) {
	t.Helper()
	l := &captureLedger{}
	return NewDispatcherWithConfig(cat, adapters, DispatcherConfig{Ledger: l}), l
}

// An upstream failure at step 7 still knows the resolved variant, so the entry
// must carry it: without variant_id a failure cannot be attributed to the
// backend that produced it.
func TestFailedDispatchWritesGainEntryWithVariant(t *testing.T) {
	cat := minimalCatalog("noop")
	upstream := NewStructuredError(ErrCodeServiceDown, "upstream refused").WithRetryable(true)
	d, l := failureLedgerDispatcher(t, cat, failingAdapter(upstream))

	_, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json", RequestID: "fail-1"})
	if err == nil {
		t.Fatal("Dispatch err = nil; the adapter refused")
	}
	if l.calls != 1 {
		t.Fatalf("ledger Append calls = %d; want 1 entry for the failed dispatch", l.calls)
	}

	e := l.entries[0]
	if e.ErrorCode != string(ErrCodeServiceDown) {
		t.Errorf("error_code = %q; want %q", e.ErrorCode, ErrCodeServiceDown)
	}
	if e.OpID != "gum.code" {
		t.Errorf("op_id = %q; want gum.code", e.OpID)
	}
	if e.VariantID == nil || *e.VariantID != "gum.code.v1.test" {
		t.Errorf("variant_id = %v; step 7 failed after routing, so the resolved variant is known", e.VariantID)
	}
	if e.RawTokens != 0 || e.ShapedTokens != 0 {
		t.Errorf("raw_tokens=%d shaped_tokens=%d; a failed call moved no payload", e.RawTokens, e.ShapedTokens)
	}
	if e.BaselineMethod != gain.BaselineEstimated {
		t.Errorf("baseline_method = %q; a live failure is never a fixture replay", e.BaselineMethod)
	}
	if e.Timestamp == "" {
		t.Error("timestamp is empty; the window filter drops undated entries")
	}
}

// A failure before routing has no variant to name. Emitting an empty string
// would assert a variant id that never existed, so the field stays null.
func TestFailedDispatchBeforeRoutingWritesNullVariant(t *testing.T) {
	d, l := failureLedgerDispatcher(t, minimalCatalog("noop"), map[string]Adapter{})

	_, err := d.Dispatch(context.Background(), &Invocation{OpID: "no.such.op", Format: "json", RequestID: "fail-2"})
	if err == nil {
		t.Fatal("Dispatch err = nil for an unknown op")
	}
	if l.calls != 1 {
		t.Fatalf("ledger Append calls = %d; want 1", l.calls)
	}

	e := l.entries[0]
	if e.ErrorCode != string(ErrCodeOpNotFound) {
		t.Errorf("error_code = %q; want %q", e.ErrorCode, ErrCodeOpNotFound)
	}
	if e.VariantID != nil {
		t.Errorf("variant_id = %q; routing never ran", *e.VariantID)
	}
	if e.OpFamily == "" {
		t.Error("op_family is empty; gum gain groups history rows by it")
	}
}

// A non-structured adapter error is wrapped as SERVICE_DOWN before it reaches
// the caller. The ledger must record the code the caller saw, not an empty
// string.
func TestFailedDispatchRecordsWrappedErrorCode(t *testing.T) {
	d, l := failureLedgerDispatcher(t, minimalCatalog("noop"), failingAdapter(errors.New("socket closed")))

	if _, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json", RequestID: "fail-3"}); err == nil {
		t.Fatal("Dispatch err = nil; the adapter refused")
	}
	if l.calls != 1 {
		t.Fatalf("ledger Append calls = %d; want 1", l.calls)
	}
	if got := l.entries[0].ErrorCode; got != string(ErrCodeServiceDown) {
		t.Errorf("error_code = %q; a bare error wraps to %q", got, ErrCodeServiceDown)
	}
}

// A successful dispatch must still write exactly one entry, and it must not
// carry an error_code.
func TestSuccessfulDispatchWritesNoErrorCode(t *testing.T) {
	adapters := map[string]Adapter{"noop": AdapterFunc(func(context.Context, *Invocation, *ResolvedVariant, *Credentials) (*Response, error) {
		return &Response{Body: []byte(`{"ok":true}`), Format: "json", StatusCode: 200}, nil
	})}
	d, l := failureLedgerDispatcher(t, minimalCatalog("noop"), adapters)

	if _, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json", RequestID: "ok-1"}); err != nil {
		t.Fatalf("Dispatch err = %v", err)
	}
	if l.calls != 1 {
		t.Fatalf("ledger Append calls = %d; want 1", l.calls)
	}
	if got := l.entries[0].ErrorCode; got != "" {
		t.Errorf("error_code = %q on a successful dispatch; want empty", got)
	}
}

// gum_parallel already writes one inner entry per element it cancelled, via
// RecordParallelBatch. That recorder cannot tell an element cancelled before
// dispatch from one cancelled inside it, so a failure entry here for the same
// element would make element_count disagree with the number of inner entries
// carrying the batch_id. The batch recorder owns the row; dispatch yields.
func TestCancelledBatchElementYieldsToTheBatchRecorder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d, l := failureLedgerDispatcher(t, minimalCatalog("noop"), failingAdapter(errors.New("unreached")))

	_, err := d.Dispatch(ctx, &Invocation{OpID: "gum.code", Format: "json", RequestID: "b-1", BatchID: "batch-1", BatchIndex: 3})
	if err == nil {
		t.Fatal("Dispatch err = nil on a cancelled context")
	}

	var se *StructuredError
	if !errors.As(err, &se) {
		t.Fatalf("error %v carries no structured code", err)
	}
	if se.ErrCode != ErrCodeCancelled {
		t.Fatalf("error_code = %q; want %q, so this test exercises the guard it names", se.ErrCode, ErrCodeCancelled)
	}
	if l.calls != 0 {
		t.Errorf("ledger Append calls = %d; RecordParallelBatch owns the cancelled element's entry, so dispatch must write none", l.calls)
	}
}

// The same cancellation outside a batch has no other recorder, so it must be
// written here or it is lost.
func TestCancelledStandaloneCallStillWritesOneEntry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d, l := failureLedgerDispatcher(t, minimalCatalog("noop"), failingAdapter(errors.New("unreached")))

	if _, err := d.Dispatch(ctx, &Invocation{OpID: "gum.code", Format: "json", RequestID: "s-1"}); err == nil {
		t.Fatal("Dispatch err = nil on a cancelled context")
	}
	if l.calls != 1 {
		t.Fatalf("ledger Append calls = %d; want 1", l.calls)
	}

	e := l.entries[0]
	if e.ErrorCode != string(ErrCodeCancelled) {
		t.Errorf("error_code = %q; want %q", e.ErrorCode, ErrCodeCancelled)
	}
	if !e.Cancelled {
		t.Error("cancelled = false; §12.3 flags a cancelled call")
	}
	if e.BatchIndex != nil {
		t.Errorf("batch_index = %d; a standalone call is in no batch", *e.BatchIndex)
	}
}

// A failed entry must not move the release subtotal. computeStats admits only
// baseline_method="fixture_replay" rows there, and a live failure is
// "estimated", so the guard is structural. This pins it.
func TestFailedEntryStaysOutOfTheReleaseSubtotal(t *testing.T) {
	d, l := failureLedgerDispatcher(t, minimalCatalog("noop"), failingAdapter(errors.New("boom")))

	if _, err := d.Dispatch(context.Background(), &Invocation{OpID: "gum.code", Format: "json", RequestID: "r-1"}); err == nil {
		t.Fatal("Dispatch err = nil; the adapter refused")
	}
	if l.calls != 1 {
		t.Fatalf("ledger Append calls = %d; want 1", l.calls)
	}
	if got := l.entries[0].BaselineMethod; got == gain.BaselineFixtureReplay {
		t.Errorf("baseline_method = %q; a failed live call must never enter the release subtotal", got)
	}
}
