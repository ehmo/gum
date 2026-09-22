// Spec §9.1 acceptance for field_mask_mode="dual_fetch": one shaped request
// plus one unmasked recovery request, both billed, with the §9.0 stage-9
// artifact holding the unmasked body and the §11 log marking the second
// request alone.

package dispatch

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/output/tee"
)

const (
	dualMask     = "messages(id)"
	dualMasked   = `{"messages":[{"id":"m1"}]}`
	dualUnmasked = `{"messages":[{"id":"m1","snippet":"full text","labelIds":["INBOX"]}]}`
)

// dualAuditSink records every §11 entry in append order, which is what the
// "second request only" assertions read.
type dualAuditSink struct {
	mu      sync.Mutex
	entries []map[string]any
}

func (s *dualAuditSink) Append(e map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
}

func (s *dualAuditSink) all() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.entries...)
}

// dualCatalog is minimalCatalog plus annotations.idempotent=true, which the
// §9.1 eligibility gate requires before dual_fetch may activate at all.
func dualCatalog(adapterKey string) *catalog.Catalog {
	c := minimalCatalog(adapterKey)
	c.Ops[0].Variants[0].Annotations = &catalog.Annotation{Idempotent: true}
	return c
}

// dualRecorder answers with the masked body when a `fields` arg is present and
// the unmasked body when it is absent, so a test can tell the two requests
// apart by their payload as well as by their args.
type dualRecorder struct {
	mu     sync.Mutex
	fields []string
	err    error // returned for the unmasked request only
}

func (r *dualRecorder) adapter() Adapter {
	return &funcAdapter{
		execute: func(_ context.Context, inv *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			mask, _ := inv.Args["fields"].(string)
			r.mu.Lock()
			r.fields = append(r.fields, mask)
			r.mu.Unlock()
			if mask == "" {
				if r.err != nil {
					return nil, r.err
				}
				return &Response{Body: []byte(dualUnmasked), Format: "json", StatusCode: 200}, nil
			}
			return &Response{Body: []byte(dualMasked), Format: "json", StatusCode: 200}, nil
		},
	}
}

func (r *dualRecorder) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.fields...)
}

type dualFixture struct {
	rec      *dualRecorder
	sink     *dualAuditSink
	dispatch Dispatcher
}

// newDualFixture wires a dispatcher whose tee mode is "always", because
// dualFetchWanted refuses to bill a recovery request no artifact will hold.
// teeMode "" selects that default; pass "off" to exercise the refusal.
func newDualFixture(t *testing.T, teeMode string, cacheOn bool) *dualFixture {
	t.Helper()
	if teeMode == "" {
		teeMode = "always"
	}
	rec := &dualRecorder{}
	sink := &dualAuditSink{}
	cfg := DispatcherConfig{
		Tee:   TeeConfig{ProfileDir: t.TempDir(), Mode: teeMode, RetentionHours: 24},
		Audit: sink,
	}
	if cacheOn {
		cfg.Cache = cache.NewMemCache(16, time.Minute)
	}
	return &dualFixture{
		rec:      rec,
		sink:     sink,
		dispatch: NewDispatcherWithConfig(dualCatalog("stub"), map[string]Adapter{"stub": rec.adapter()}, cfg),
	}
}

// dualInvocation asks for dual_fetch with a profile-supplied field_mask, which
// lifecycle step 3c injects as the upstream `fields` arg.
func dualInvocation(mask string) *Invocation {
	return &Invocation{
		OpID:                   "gum.code",
		Args:                   map[string]any{},
		Format:                 "json",
		RequestID:              "dual-fetch",
		AuthSubjectFingerprint: "fp-dual",
		OutputProfile: &profile.Profile{
			FieldMaskMode: profile.FieldMaskModeDualFetch,
			FieldMask:     mask,
			Recovery:      "local_artifact",
		},
	}
}

// TestDualFetchIssuesSecondUnmaskedRequest is the core §9.1 promise: the
// shaped request carries the mask, and a second request repeats it with no
// projection at all.
func TestDualFetchIssuesSecondUnmaskedRequest(t *testing.T) {
	fx := newDualFixture(t, "", false)

	if _, err := fx.dispatch.Dispatch(context.Background(), dualInvocation(dualMask)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	got := fx.rec.calls()
	if len(got) != 2 {
		t.Fatalf("adapter calls = %d (%q); want 2 (shaped + unmasked recovery)", len(got), got)
	}
	if got[0] != dualMask {
		t.Errorf("first request fields = %q; want the profile mask %q", got[0], dualMask)
	}
	if got[1] != "" {
		t.Errorf("second request fields = %q; want no projection at all", got[1])
	}
}

// TestDualFetchArtifactHoldsUnmaskedBody pins spec §9.1's stage-9 note: the
// artifact captures the UNMASKED second fetch, not the shaped first-fetch
// tree. Teeing the masked body would hand back a full_result_path that claims
// pre-mask recovery and cannot deliver it.
func TestDualFetchArtifactHoldsUnmaskedBody(t *testing.T) {
	fx := newDualFixture(t, "", false)

	res, err := fx.dispatch.Dispatch(context.Background(), dualInvocation(dualMask))
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if res.FullResultPath == "" {
		t.Fatal("FullResultPath empty; dual_fetch must produce a recovery artifact")
	}
	payload, err := tee.Read(res.FullResultPath)
	if err != nil {
		t.Fatalf("tee.Read(%s): %v", res.FullResultPath, err)
	}
	if string(payload) != dualUnmasked {
		t.Errorf("artifact = %s; want the unmasked body %s", payload, dualUnmasked)
	}
	if res.FullResultSize == nil || *res.FullResultSize != int64(len(dualUnmasked)) {
		t.Errorf("FullResultSize = %v; want %d (the unmasked byte count)", res.FullResultSize, len(dualUnmasked))
	}
}

// TestDualFetchAuditRecordsSecondRequestOnly pins §11: both requests are
// logged, in request order, and only the unmasked one carries dual_fetch. The
// two entries must also differ by args_hash, because they went on the wire
// with different args.
func TestDualFetchAuditRecordsSecondRequestOnly(t *testing.T) {
	fx := newDualFixture(t, "", false)

	if _, err := fx.dispatch.Dispatch(context.Background(), dualInvocation(dualMask)); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	entries := fx.sink.all()
	if len(entries) != 2 {
		t.Fatalf("audit entries = %d; want 2 (both requests count against the audit log)", len(entries))
	}
	if _, present := entries[0]["dual_fetch"]; present {
		t.Errorf("masked request carries dual_fetch; §11 reserves it for the unmasked one: %v", entries[0])
	}
	if entries[1]["dual_fetch"] != true {
		t.Errorf("unmasked request dual_fetch = %v; want true", entries[1]["dual_fetch"])
	}
	if entries[0]["args_hash"] == entries[1]["args_hash"] {
		t.Errorf("both entries share args_hash %v; the unmasked request dropped `fields` and must hash differently",
			entries[0]["args_hash"])
	}
}

// TestDualFetchLeavesInvocationArgsIntact guards the reason dualFetch clones
// instead of editing: inv.Args feeds the §10.3 cache key, the §11 args_hash
// and the §9.0 tee hash. Deleting `fields` in place would re-point all three
// at an invocation the caller never made.
func TestDualFetchLeavesInvocationArgsIntact(t *testing.T) {
	fx := newDualFixture(t, "", false)
	inv := dualInvocation(dualMask)

	if _, err := fx.dispatch.Dispatch(context.Background(), inv); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got, _ := inv.Args["fields"].(string); got != dualMask {
		t.Errorf("inv.Args[fields] = %q after dispatch; want the mask %q left in place", got, dualMask)
	}
}

// TestDualFetchSkippedWithoutMaskOnWire: with no `fields` arg the "unmasked"
// request would return the bytes the kernel already holds, so issuing it would
// bill a second upstream call for nothing.
func TestDualFetchSkippedWithoutMaskOnWire(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*Invocation)
	}{
		{"no field_mask anywhere", func(*Invocation) {}},
		{"--no-field-mask", func(inv *Invocation) {
			inv.OutputProfile.FieldMask = dualMask
			inv.SuppressFieldMask = true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := newDualFixture(t, "", false)
			inv := dualInvocation("")
			tc.mut(inv)

			if _, err := fx.dispatch.Dispatch(context.Background(), inv); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			if got := fx.rec.calls(); len(got) != 1 {
				t.Fatalf("adapter calls = %d (%q); want 1", len(got), got)
			}
			for _, e := range fx.sink.all() {
				if _, present := e["dual_fetch"]; present {
					t.Errorf("audit entry carries dual_fetch with no second request: %v", e)
				}
			}
		})
	}
}

// TestDualFetchSkippedWhenNoArtifactWouldBeWritten: §9.1 exists to feed §9.0
// stage 9. With tee_mode="off" the unmasked body would be fetched and dropped,
// so the request is never made.
func TestDualFetchSkippedWhenNoArtifactWouldBeWritten(t *testing.T) {
	for _, mode := range []string{"off", "failures"} {
		t.Run(mode, func(t *testing.T) {
			fx := newDualFixture(t, mode, false)

			if _, err := fx.dispatch.Dispatch(context.Background(), dualInvocation(dualMask)); err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			if got := fx.rec.calls(); len(got) != 1 {
				t.Errorf("adapter calls = %d (%q) under tee_mode=%q; want 1", len(got), got, mode)
			}
		})
	}
}

// TestDualFetchFailureKeepsPrimaryResponse: the shaped request already
// succeeded, so a failed recovery request must not fail the call. It must also
// not fall back to teeing the masked body, which would answer a pre-mask
// recovery promise with post-mask data the caller cannot distinguish.
func TestDualFetchFailureKeepsPrimaryResponse(t *testing.T) {
	fx := newDualFixture(t, "", false)
	fx.rec.err = errors.New("upstream 503 on the recovery request")

	res, err := fx.dispatch.Dispatch(context.Background(), dualInvocation(dualMask))
	if err != nil {
		t.Fatalf("Dispatch returned %v; a failed recovery fetch must not fail the call", err)
	}
	if !strings.Contains(string(res.Body), "m1") {
		t.Errorf("body = %s; want the successful shaped response", res.Body)
	}
	if res.FullResultPath != "" {
		if _, serr := os.Stat(res.FullResultPath); serr == nil {
			t.Errorf("wrote artifact %s after the recovery fetch failed; it would hold masked data", res.FullResultPath)
		}
	}
	joined := strings.Join(res.ValidationWarnings, "|")
	if !strings.Contains(joined, "dual_fetch") {
		t.Errorf("validation warnings = %q; want one naming the failed dual_fetch recovery", res.ValidationWarnings)
	}
	entries := fx.sink.all()
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d; want 1 (the failed request is not a completed unmasked fetch)", len(entries))
	}
	if _, present := entries[0]["dual_fetch"]; present {
		t.Errorf("audit entry carries dual_fetch after the second request failed: %v", entries[0])
	}
}

// TestDualFetchOnCacheHitStillRecovers: the §10.3 key carries the mask, so a
// hit serves the masked body. Teeing that under a dual_fetch profile would
// break the same promise the cold path keeps, so the warm path issues the
// recovery request too — one upstream call, not zero.
func TestDualFetchOnCacheHitStillRecovers(t *testing.T) {
	fx := newDualFixture(t, "", true)

	if _, err := fx.dispatch.Dispatch(context.Background(), dualInvocation(dualMask)); err != nil {
		t.Fatalf("cold Dispatch: %v", err)
	}
	cold := len(fx.rec.calls())
	if cold != 2 {
		t.Fatalf("cold adapter calls = %d; want 2", cold)
	}

	warm, err := fx.dispatch.Dispatch(context.Background(), dualInvocation(dualMask))
	if err != nil {
		t.Fatalf("warm Dispatch: %v", err)
	}
	got := fx.rec.calls()
	if len(got) != 3 {
		t.Fatalf("total adapter calls = %d (%q); want 3 (cold pair + one warm recovery request)", len(got), got)
	}
	if got[2] != "" {
		t.Errorf("warm request fields = %q; want the unmasked recovery request", got[2])
	}
	payload, err := tee.Read(warm.FullResultPath)
	if err != nil {
		t.Fatalf("tee.Read(%s): %v", warm.FullResultPath, err)
	}
	if string(payload) != dualUnmasked {
		t.Errorf("warm artifact = %s; want the unmasked body", payload)
	}
	entries := fx.sink.all()
	if len(entries) != 4 {
		t.Fatalf("audit entries = %d; want 4 (two per dispatch)", len(entries))
	}
	if entries[3]["dual_fetch"] != true {
		t.Errorf("warm second entry dual_fetch = %v; want true", entries[3]["dual_fetch"])
	}
}
