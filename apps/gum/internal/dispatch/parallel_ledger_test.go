package dispatch

import (
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/gain"
)

// §12.3 batch accounting (beads gum-7oap, gum-z8u9; docs/test-matrix.md).
// One gum_parallel call writes an outer sentinel entry plus one
// inner entry per element. Step 9 supplies the inner entries for elements
// that dispatched; RecordParallelBatch supplies the outer entry and the inner
// entries for elements that were cancelled before producing a response.

func TestBuildGainEntryCarriesBatchLinkage(t *testing.T) {
	d := &dispatcher{}
	inv := &Invocation{
		OpID:       "gmail.users.messages.list",
		Args:       map[string]any{"q": "test"},
		BatchID:    "a1b2c3d4",
		BatchIndex: 2,
	}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v.1"}}

	e := d.buildGainEntry(inv, rv, nil, &ShapedResponse{Body: []byte("x")}, &Response{Body: []byte("xxxx")}, false, false)

	if e.BatchID != "a1b2c3d4" {
		t.Errorf("BatchID = %q; want the batch's id, or this entry joins no batch", e.BatchID)
	}
	if e.BatchIndex == nil {
		t.Fatal("BatchIndex is nil for a batched invocation")
	}
	if *e.BatchIndex != 2 {
		t.Errorf("BatchIndex = %d; want 2", *e.BatchIndex)
	}
}

func TestBuildGainEntryOmitsBatchFieldsOutsideBatch(t *testing.T) {
	d := &dispatcher{}
	inv := &Invocation{OpID: "gmail.users.messages.list", BatchIndex: 7}

	e := d.buildGainEntry(inv, nil, nil, &ShapedResponse{Body: []byte("x")}, &Response{Body: []byte("xxxx")}, false, false)

	if e.BatchID != "" {
		t.Errorf("BatchID = %q; a standalone call belongs to no batch", e.BatchID)
	}
	if e.BatchIndex != nil {
		t.Errorf("BatchIndex = %d; a standalone call has no position in a batch", *e.BatchIndex)
	}
}

// batchFixture records one two-element batch whose second element was
// cancelled, and returns the entries the ledger received.
func batchFixture(t *testing.T, cancelled bool) []gain.Entry {
	t.Helper()

	l := &captureLedger{}
	d := &dispatcher{gainLedger: l}
	d.RecordParallelBatch(ParallelBatch{
		BatchID: "feedface",
		Args:    map[string]any{"elements": []any{map[string]any{"op_id": "gmail.users.messages.list"}}},
		Envelope: map[string]any{
			"format":   "parallel_results",
			"batch_id": "feedface",
			"results":  []any{map[string]any{"_idx": 0}},
		},
		Elements: []ParallelBatchElement{
			{OpID: "gmail.users.messages.list", Args: map[string]any{"q": "a"}},
			{OpID: "calendar.events.list", Args: map[string]any{"q": "b"}, Cancelled: true},
		},
		Cancelled: cancelled,
	})
	return l.entries
}

func TestRecordParallelBatchWritesOuterSentinel(t *testing.T) {
	entries := batchFixture(t, false)
	if len(entries) == 0 {
		t.Fatal("RecordParallelBatch wrote nothing")
	}
	outer := entries[0]

	if outer.OpID != gain.OpIDGumParallel || outer.OpFamily != gain.OpIDGumParallel {
		t.Errorf("outer op_id/op_family = %q/%q; want %q", outer.OpID, outer.OpFamily, gain.OpIDGumParallel)
	}
	if outer.VariantID != nil || outer.OutputProfile != nil {
		t.Error("outer variant_id/output_profile must be null; a batch resolves neither")
	}
	if outer.AuthSubjectFingerprint != "batch" {
		t.Errorf("outer auth_subject_fingerprint = %q; want %q", outer.AuthSubjectFingerprint, "batch")
	}
	if outer.BatchID != "feedface" {
		t.Errorf("outer batch_id = %q; want %q", outer.BatchID, "feedface")
	}
	if outer.ElementCount == nil || *outer.ElementCount != 2 {
		t.Errorf("outer element_count = %v; want 2", outer.ElementCount)
	}
	if outer.RawTokens != 0 {
		t.Errorf("outer raw_tokens = %d; the envelope has no upstream body, so it must be 0", outer.RawTokens)
	}
	if outer.ShapedTokens != outer.RequestTokens+outer.ResponseTokens {
		t.Errorf("outer shaped_tokens = %d; want request+response = %d",
			outer.ShapedTokens, outer.RequestTokens+outer.ResponseTokens)
	}
	if outer.ShapedTokens == 0 {
		t.Error("outer shaped_tokens = 0; the envelope cost is what batch_envelope_overhead reports")
	}
	if outer.ArgsHash == "" {
		t.Error("outer args_hash is empty")
	}
	if outer.CacheStatus != "not_applicable" || outer.FieldMaskStatus != "not_applicable" {
		t.Errorf("outer cache/field_mask status = %q/%q; want not_applicable", outer.CacheStatus, outer.FieldMaskStatus)
	}
	if outer.BaselineMethod != gain.BaselineEstimated {
		t.Errorf("outer baseline_method = %q; a live batch is not fixture-backed", outer.BaselineMethod)
	}
	if outer.Timestamp == "" {
		t.Error("outer entry has no ts")
	}
	if outer.Cancelled {
		t.Error("outer entry marked cancelled for a batch that completed")
	}
}

func TestRecordParallelBatchWritesCancelledInnerEntry(t *testing.T) {
	entries := batchFixture(t, false)

	// One outer entry, plus one inner entry for the cancelled element only:
	// the element that dispatched already wrote its own at step 9.
	if len(entries) != 2 {
		t.Fatalf("wrote %d entries; want 2 (outer + the cancelled element)", len(entries))
	}
	inner := entries[1]

	if inner.OpID != "calendar.events.list" {
		t.Errorf("inner op_id = %q; want the cancelled element's", inner.OpID)
	}
	if inner.BatchID != "feedface" {
		t.Errorf("inner batch_id = %q; want the outer entry's %q", inner.BatchID, "feedface")
	}
	if inner.BatchIndex == nil || *inner.BatchIndex != 1 {
		t.Errorf("inner batch_index = %v; want 1", inner.BatchIndex)
	}
	if !inner.Cancelled {
		t.Error("inner entry not marked cancelled")
	}
	if inner.ErrorCode != "CANCELLED" {
		t.Errorf("inner error_code = %q; want CANCELLED", inner.ErrorCode)
	}
	if inner.RawTokens != 0 || inner.ShapedTokens != 0 || inner.RequestTokens != 0 || inner.ResponseTokens != 0 {
		t.Error("cancelled element priced with non-zero tokens; it sent and received nothing")
	}
	if inner.VariantID != nil || inner.OutputProfile != nil {
		t.Error("cancelled element must carry null variant_id/output_profile; it never routed")
	}
}

func TestRecordParallelBatchMarksOuterCancelled(t *testing.T) {
	entries := batchFixture(t, true)
	if !entries[0].Cancelled {
		t.Error("outer entry not marked cancelled after the batch context was cancelled")
	}
}

func TestRecordParallelBatchNoLedgerIsNoop(t *testing.T) {
	d := &dispatcher{}
	d.RecordParallelBatch(ParallelBatch{BatchID: "feedface"})
}

func TestRecordParallelBatchWithoutBatchIDIsNoop(t *testing.T) {
	l := &captureLedger{}
	d := &dispatcher{gainLedger: l}

	d.RecordParallelBatch(ParallelBatch{Elements: []ParallelBatchElement{{OpID: "op.a"}}})

	if l.calls != 0 {
		t.Errorf("Append calls = %d; an unidentified batch links nothing and must not be recorded", l.calls)
	}
}

// TestDispatcherSatisfiesParallelBatchRecorder pins the capability the
// gum_parallel adapter type-asserts for. A rename on either side silently
// disables batch accounting, since the assertion just fails.
func TestDispatcherSatisfiesParallelBatchRecorder(t *testing.T) {
	var d Dispatcher = &dispatcher{}
	if _, ok := d.(ParallelBatchRecorder); !ok {
		t.Fatal("*dispatcher no longer implements ParallelBatchRecorder; gum_parallel would record no batches")
	}
}
