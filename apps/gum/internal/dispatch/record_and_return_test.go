package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// captureLedger records every Append call made by step 9.
type captureLedger struct {
	calls   int
	entries []GainEntry
}

func (c *captureLedger) Append(e GainEntry) error {
	c.calls++
	c.entries = append(c.entries, e)
	return nil
}

// TestRecordAndReturnNilLedgerNoop verifies a dispatcher with no gain ledger
// returns the shaped response untouched (Phase 2 fallback behavior).
func TestRecordAndReturnNilLedgerNoop(t *testing.T) {
	d := &dispatcher{}
	shaped := &ShapedResponse{Body: []byte("hello"), Format: "raw"}
	inv := &Invocation{OpID: "x"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "x.v1"}}

	out, err := d.recordAndReturn(context.Background(), inv, rv, shaped, &Response{Body: []byte("hello")}, time.Now(), false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != shaped {
		t.Error("nil ledger: shaped response must pass through unchanged")
	}
}

// TestRecordAndReturnLedgerEntryPopulated verifies the gain ledger receives an
// entry with op_id, variant_id, format, bytes_in, bytes_out, and timestamp.
// Acceptance: gain ledger entry recorded with bytes_in/bytes_out/savings.
func TestRecordAndReturnLedgerEntryPopulated(t *testing.T) {
	l := &captureLedger{}
	d := &dispatcher{gainLedger: l}

	rawBody := []byte(`[{"a":1,"b":2,"c":3,"d":4}]`)
	shaped := &ShapedResponse{Body: []byte("a,b,c,d\n1,2,3,4"), Format: "toon"}
	inv := &Invocation{OpID: "gmail.messages.list", Args: map[string]any{"q": "test"}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v.1"}}

	_, err := d.recordAndReturn(context.Background(), inv, rv, shaped, &Response{Body: rawBody}, time.Now().Add(-25*time.Millisecond), false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if l.calls != 1 {
		t.Fatalf("Append calls=%d want 1", l.calls)
	}
	e := l.entries[0]
	if e.OpID != "gmail.messages.list" {
		t.Errorf("OpID=%q want gmail.messages.list", e.OpID)
	}
	if e.VariantID != "v.1" {
		t.Errorf("VariantID=%q want v.1", e.VariantID)
	}
	if e.Format != "toon" {
		t.Errorf("Format=%q want toon", e.Format)
	}
	if e.BytesIn != len(rawBody) {
		t.Errorf("BytesIn=%d want %d", e.BytesIn, len(rawBody))
	}
	if e.BytesOut != len(shaped.Body) {
		t.Errorf("BytesOut=%d want %d", e.BytesOut, len(shaped.Body))
	}
	if e.WallMs <= 0 {
		t.Errorf("WallMs=%d want >0 (start was 25ms ago)", e.WallMs)
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp must be set")
	}
	if e.CacheHit {
		t.Error("CacheHit=true for non-cache-hit path")
	}
}

// TestRecordAndReturnCacheHitFlagged verifies a cache-hit path sets
// CacheHit=true on the ledger entry (so gain analytics can distinguish
// served-from-cache from upstream-fetched).
func TestRecordAndReturnCacheHitFlagged(t *testing.T) {
	l := &captureLedger{}
	d := &dispatcher{gainLedger: l}

	shaped := &ShapedResponse{Body: []byte("ok"), Format: "json"}
	inv := &Invocation{OpID: "x"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "x.v1"}}
	_, err := d.recordAndReturn(context.Background(), inv, rv, shaped, &Response{Body: []byte("ok")}, time.Now(), true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !l.entries[0].CacheHit {
		t.Error("CacheHit not propagated to ledger entry")
	}
}

// TestRecordAndReturnLedgerErrorDoesNotFailDispatch verifies a ledger Append
// error is logged but does NOT mask the successful response — accounting is
// best-effort per spec (audit/gain should never fail a successful call).
func TestRecordAndReturnLedgerErrorDoesNotFailDispatch(t *testing.T) {
	d := &dispatcher{gainLedger: &erroringLedger{}}
	shaped := &ShapedResponse{Body: []byte("ok"), Format: "json"}
	inv := &Invocation{OpID: "x"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "x.v1"}}

	out, err := d.recordAndReturn(context.Background(), inv, rv, shaped, &Response{Body: []byte("ok")}, time.Now(), false)
	if err != nil {
		t.Fatalf("ledger error must not fail dispatch: %v", err)
	}
	if out != shaped {
		t.Error("ledger error must not lose shaped response")
	}
}

type erroringLedger struct{}

func (erroringLedger) Append(_ GainEntry) error { return errStub }

var errStub = stubErr("synthetic ledger failure")

type stubErr string

func (s stubErr) Error() string { return string(s) }
