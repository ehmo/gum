package dispatch

import (
	"context"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/gain"
	"github.com/ehmo/gum/internal/output/profile"
)

// captureLedger records every Append call made by step 9.
type captureLedger struct {
	calls   int
	entries []gain.Entry
}

func (c *captureLedger) Append(e gain.Entry) error {
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

	out, err := d.recordAndReturn(context.Background(), inv, rv, nil, shaped, &Response{Body: []byte("hello")}, false, false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != shaped {
		t.Error("nil ledger: shaped response must pass through unchanged")
	}
}

// TestRecordAndReturnLedgerEntryPopulated verifies the spec §12.3 entry step 9
// appends. The two counts the gain report is built from are raw_tokens (the
// upstream body) and shaped_tokens (what the caller receives); their difference
// is the savings figure, so both must be real token counts, not byte lengths.
func TestRecordAndReturnLedgerEntryPopulated(t *testing.T) {
	l := &captureLedger{}
	d := &dispatcher{gainLedger: l}

	rawBody := []byte(`[{"a":1,"b":2,"c":3,"d":4}]`)
	shaped := &ShapedResponse{
		Body:       []byte("a,b,c,d\n1,2,3,4"),
		Format:     "toon",
		Expression: &ExpressionMeta{Profile: "gmail.compact"},
	}
	inv := &Invocation{
		OpID:                   "gmail.users.messages.list",
		Args:                   map[string]any{"q": "test"},
		AuthSubjectFingerprint: "fp-1",
	}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "v.1"}}

	_, err := d.recordAndReturn(context.Background(), inv, rv, nil, shaped, &Response{Body: rawBody}, false, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if l.calls != 1 {
		t.Fatalf("Append calls=%d want 1", l.calls)
	}
	e := l.entries[0]

	if e.OpID != "gmail.users.messages.list" {
		t.Errorf("OpID=%q want gmail.users.messages.list", e.OpID)
	}
	if e.OpFamily != "gmail.users.messages" {
		t.Errorf("OpFamily=%q want gmail.users.messages", e.OpFamily)
	}
	if e.VariantID == nil || *e.VariantID != "v.1" {
		t.Errorf("VariantID=%v want v.1", e.VariantID)
	}
	if e.OutputProfile == nil || *e.OutputProfile != "gmail.compact" {
		t.Errorf("OutputProfile=%v want gmail.compact", e.OutputProfile)
	}
	if e.AuthSubjectFingerprint != "fp-1" {
		t.Errorf("AuthSubjectFingerprint=%q want fp-1", e.AuthSubjectFingerprint)
	}
	if len(e.Session) != 8 {
		t.Errorf("Session=%q want 8 hex chars", e.Session)
	}
	if e.ArgsHash == "" {
		t.Error("ArgsHash must be set: the §12.3 entry keys on it")
	}
	if e.RawTokens <= 0 {
		t.Errorf("RawTokens=%d want >0 for a %d-byte upstream body", e.RawTokens, len(rawBody))
	}
	if e.ShapedTokens <= 0 {
		t.Errorf("ShapedTokens=%d want >0", e.ShapedTokens)
	}
	if e.RawTokens <= e.ShapedTokens {
		t.Errorf("RawTokens=%d ShapedTokens=%d: this fixture shapes JSON down to TOON, so raw must exceed shaped",
			e.RawTokens, e.ShapedTokens)
	}
	if e.ResponseTokens != e.ShapedTokens {
		t.Errorf("ResponseTokens=%d ShapedTokens=%d: the caller receives the shaped body", e.ResponseTokens, e.ShapedTokens)
	}
	if e.RequestTokens <= 0 {
		t.Errorf("RequestTokens=%d want >0 for args %v", e.RequestTokens, inv.Args)
	}
	if e.BaselineMethod != "estimated" {
		t.Errorf("BaselineMethod=%q want estimated for a live dispatch", e.BaselineMethod)
	}
	if e.CacheStatus != "miss" {
		t.Errorf("CacheStatus=%q want miss", e.CacheStatus)
	}
	if e.Timestamp == "" {
		t.Error("Timestamp must be set")
	}
	if e.ServedFromCache {
		t.Error("ServedFromCache=true for non-cache-hit path")
	}
	if e.IsRetry {
		t.Error("IsRetry=true on the first call of this args_hash")
	}
}

// TestRecordAndReturnCacheHitFlagged verifies a cache-hit path sets
// served_from_cache and cache_status="hit", so gain analytics can separate
// served-from-cache entries from upstream-fetched ones.
func TestRecordAndReturnCacheHitFlagged(t *testing.T) {
	l := &captureLedger{}
	d := &dispatcher{gainLedger: l}

	shaped := &ShapedResponse{Body: []byte("ok"), Format: "json"}
	inv := &Invocation{OpID: "x"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "x.v1"}}
	_, err := d.recordAndReturn(context.Background(), inv, rv, nil, shaped, &Response{Body: []byte("ok")}, true, true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !l.entries[0].ServedFromCache {
		t.Error("ServedFromCache not propagated to ledger entry")
	}
	if got := l.entries[0].CacheStatus; got != "hit" {
		t.Errorf("CacheStatus=%q want hit", got)
	}
}

// TestRecordAndReturnCacheIneligibleIsNotAMiss verifies an op that was never a
// cache candidate reports "not_applicable". Recording it as a miss would make
// every write look like a cache the kernel failed to warm.
func TestRecordAndReturnCacheIneligibleIsNotAMiss(t *testing.T) {
	l := &captureLedger{}
	d := &dispatcher{gainLedger: l}

	shaped := &ShapedResponse{Body: []byte("ok"), Format: "json"}
	inv := &Invocation{OpID: "gmail.users.messages.send"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "x.v1"}}
	if _, err := d.recordAndReturn(context.Background(), inv, rv, nil, shaped, &Response{Body: []byte("ok")}, false, false); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := l.entries[0].CacheStatus; got != "not_applicable" {
		t.Errorf("CacheStatus=%q want not_applicable", got)
	}
}

// TestRecordAndReturnMarksRetry verifies the §12.3 is_retry window: the same
// session repeating one op_family + args_hash inside 5 minutes is a retry.
func TestRecordAndReturnMarksRetry(t *testing.T) {
	l := &captureLedger{}
	d := &dispatcher{gainLedger: l}

	shaped := &ShapedResponse{Body: []byte("ok"), Format: "json"}
	inv := &Invocation{OpID: "gmail.users.messages.list", Args: map[string]any{"q": "same"}}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "x.v1"}}

	for range 2 {
		if _, err := d.recordAndReturn(context.Background(), inv, rv, nil, shaped, &Response{Body: []byte("ok")}, false, true); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	}
	if l.entries[0].IsRetry {
		t.Error("first call must not be a retry")
	}
	if !l.entries[1].IsRetry {
		t.Error("second identical call inside the 5-minute window must be a retry")
	}

	// A different args_hash in the same family is a new call, not a retry.
	other := &Invocation{OpID: "gmail.users.messages.list", Args: map[string]any{"q": "different"}}
	if _, err := d.recordAndReturn(context.Background(), other, rv, nil, shaped, &Response{Body: []byte("ok")}, false, true); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if l.entries[2].IsRetry {
		t.Error("a different args_hash must not count as a retry")
	}
}

// TestRecordAndReturnFieldMaskStatus verifies the §12.3 field_mask_status enum
// distinguishes a mask that went upstream from one the profile declared and the
// call did not send (--no-field-mask) from an op with no mask at all.
func TestRecordAndReturnFieldMaskStatus(t *testing.T) {
	shaped := &ShapedResponse{Body: []byte("ok"), Format: "json"}
	rv := &ResolvedVariant{Variant: &catalog.Variant{VariantID: "x.v1"}}

	cases := []struct {
		name string
		inv  *Invocation
		want string
	}{
		{
			name: "fields_arg_sent_upstream",
			inv:  &Invocation{OpID: "a.b.list", Args: map[string]any{"fields": "messages(id)"}},
			want: "applied",
		},
		{
			name: "profile_declares_projection_but_no_fields_arg",
			inv: &Invocation{
				OpID:          "a.b.list",
				OutputProfile: &profile.Profile{Projection: []string{"id"}},
			},
			want: "skipped",
		},
		{
			name: "no_mask_anywhere",
			inv:  &Invocation{OpID: "a.b.list"},
			want: "not_applicable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &captureLedger{}
			d := &dispatcher{gainLedger: l}
			if _, err := d.recordAndReturn(context.Background(), tc.inv, rv, nil, shaped, &Response{Body: []byte("ok")}, false, true); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got := l.entries[0].FieldMaskStatus; got != tc.want {
				t.Errorf("FieldMaskStatus=%q want %q", got, tc.want)
			}
		})
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

	out, err := d.recordAndReturn(context.Background(), inv, rv, nil, shaped, &Response{Body: []byte("ok")}, false, false)
	if err != nil {
		t.Fatalf("ledger error must not fail dispatch: %v", err)
	}
	if out != shaped {
		t.Error("ledger error must not lose shaped response")
	}
}

type erroringLedger struct{}

func (erroringLedger) Append(_ gain.Entry) error { return errStub }

var errStub = stubErr("synthetic ledger failure")

type stubErr string

func (s stubErr) Error() string { return string(s) }
