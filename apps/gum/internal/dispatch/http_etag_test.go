package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/output/tee"
)

// memHTTPStore is an in-memory §10.2 backend. The production store is bbolt,
// which takes a file lock; a map keeps these tests independent of the disk and
// of each other.
type memHTTPStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemHTTPStore() *memHTTPStore {
	return &memHTTPStore{m: map[string][]byte{}}
}

func (s *memHTTPStore) Get(key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *memHTTPStore) Set(key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), value...)
	return nil
}

// etagTestCatalog is one read-class op with two variants, so a test can change
// the resolved variant without changing anything else about the call.
func etagTestCatalog(opID, adapterKey string) *catalog.Catalog {
	mkVariant := func(id string) catalog.Variant {
		return catalog.Variant{
			VariantID:     id,
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindDiscoveryREST,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     catalog.RiskClassRead,
			Binding: &catalog.Binding{
				BindingSchemaVersion: 1,
				AdapterKey:           adapterKey,
				OperationKey:         opID,
			},
		}
	}
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{{
			OpID:             opID,
			OpSchemaVersion:  1,
			Title:            "List events",
			Summary:          "List calendar events.",
			DefaultVariantID: "v1",
			Variants:         []catalog.Variant{mkVariant("v1"), mkVariant("v2")},
		}},
	}
}

// etagFixture is one dispatcher wired to a §10.2 cache, a capture ledger and a
// tee directory, plus the counters its adapter records.
type etagFixture struct {
	dispatch    Dispatcher
	ledger      *captureLedger
	teeDir      string
	calls       *atomic.Int32
	conditional *atomic.Int32
	validators  []string
}

const (
	etagOpID      = "calendar.events.list"
	etagAdapter   = "test.adapter"
	etagValidator = `W/"v1-abc"`
)

// etagFullBody is the representation upstream sends on a cold read. It is a
// realistic list page rather than a two-element stub, because the §2030 claim
// under test is that a 304 saves the caller the whole body: a body smaller
// than the 39-byte `{"unchanged":true,"etag":...}` envelope cannot show that.
var etagFullBody = func() string {
	items := make([]map[string]any, 0, 24)
	for i := 0; i < 24; i++ {
		items = append(items, map[string]any{
			"id":      fmt.Sprintf("event-%02d", i),
			"summary": fmt.Sprintf("Weekly planning session %02d", i),
			"start":   map[string]any{"dateTime": "2026-05-19T14:30:00Z"},
			"end":     map[string]any{"dateTime": "2026-05-19T15:30:00Z"},
			"status":  "confirmed",
		})
	}
	b, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		panic(err)
	}
	return string(b)
}()

func newETagFixture(t *testing.T) *etagFixture {
	t.Helper()
	fx := &etagFixture{
		ledger:      &captureLedger{},
		teeDir:      filepath.Join(t.TempDir(), "default"),
		calls:       &atomic.Int32{},
		conditional: &atomic.Int32{},
	}
	var mu sync.Mutex
	adapter := AdapterFunc(func(_ context.Context, inv *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
		fx.calls.Add(1)
		mu.Lock()
		fx.validators = append(fx.validators, inv.IfNoneMatch)
		mu.Unlock()
		if inv.IfNoneMatch == etagValidator {
			// Upstream's answer to a conditional request it can satisfy: no
			// body, the validator it matched, and 304.
			fx.conditional.Add(1)
			return &Response{Format: "json", StatusCode: http.StatusNotModified, ETag: etagValidator}, nil
		}
		return &Response{
			Body:       []byte(etagFullBody),
			Format:     "json",
			StatusCode: http.StatusOK,
			ETag:       etagValidator,
		}, nil
	})
	fx.dispatch = NewDispatcherWithConfig(
		etagTestCatalog(etagOpID, etagAdapter),
		map[string]Adapter{etagAdapter: adapter},
		DispatcherConfig{
			HTTPCache: cache.NewHTTPCache(newMemHTTPStore()),
			Ledger:    fx.ledger,
			Tee:       TeeConfig{ProfileDir: fx.teeDir, RetentionHours: 24},
		},
	)
	return fx
}

// teeFileCount counts the artifacts written under the fixture's tee directory.
func (fx *etagFixture) teeFileCount(t *testing.T) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(filepath.Join(fx.teeDir, "tee"), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("walk tee dir: %v", err)
	}
	return n
}

// etagInvocation is a read of the same resource under a named principal and
// variant, shaped by a lossy profile so the tee stage has a reason to fire.
func etagInvocation(variantID, fingerprint string) *Invocation {
	return &Invocation{
		OpID:                   etagOpID,
		RequestedVariantID:     variantID,
		Args:                   map[string]any{"calendarId": "primary"},
		Format:                 "json",
		AuthSubjectFingerprint: fingerprint,
		OutputProfile:          &profile.Profile{Recovery: "local_artifact"},
	}
}

// TestDiffOnlyModeEtagReplay is the named acceptance for two claims.
//
// test-matrix row 67 (spec §2024-2031): a 304 short-circuits the expression
// pipeline, the ledger records cache_status "etag_304" with response_tokens 0,
// `_expression` is absent from the 304 response, and a second call that
// differs only in resolved variant or credential subject MUST NOT revalidate,
// because those are key components.
//
// gum-y1n: "TestDiffOnlyModeEtagReplay extended to cover date normalization;
// cache hit rate improves in Calendar sessions." With NormalizeDatetimes=true,
// two Dispatches passing the SAME instant in DIFFERENT RFC 3339
// representations collapse to one cache key, so the adapter runs once. Without
// the flag (Rule 3 verbatim), the second call misses and the adapter runs
// again.
func TestDiffOnlyModeEtagReplay(t *testing.T) {
	const opID = "calendar.events.list"
	const adapterKey = "test.adapter"

	mkCatalog := func() *catalog.Catalog {
		return &catalog.Catalog{
			CatalogSchemaVersion: 1,
			Ops: []catalog.Op{{
				OpID:             opID,
				OpSchemaVersion:  1,
				Title:            "List events",
				Summary:          "List calendar events.",
				DefaultVariantID: "v1",
				Variants: []catalog.Variant{{
					VariantID:     "v1",
					Stability:     catalog.StabilityStable,
					InterfaceKind: catalog.InterfaceKindDiscoveryREST,
					BackendKind:   catalog.BackendKindTypedRestSDK,
					RiskClass:     catalog.RiskClassRead,
					Binding: &catalog.Binding{
						BindingSchemaVersion: 1,
						AdapterKey:           adapterKey,
						OperationKey:         opID,
					},
				}},
			}},
		}
	}

	mkInv := func(timeMin string) *Invocation {
		return &Invocation{
			OpID:   opID,
			Args:   map[string]any{"timeMin": timeMin},
			Format: "json",
		}
	}

	// Sub-test: normalization ON → second equivalent-instant call hits cache.
	t.Run("normalize_on_collapses_equivalent_instants", func(t *testing.T) {
		var calls atomic.Int32
		adapter := AdapterFunc(func(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			calls.Add(1)
			return &Response{Body: []byte(`{"items":[]}`), Format: "json", StatusCode: 200}, nil
		})
		mc := cache.NewMemCache(100, time.Minute)
		disp := NewDispatcherWithConfig(mkCatalog(), map[string]Adapter{adapterKey: adapter},
			DispatcherConfig{Cache: mc, NormalizeDatetimes: true})

		if _, err := disp.Dispatch(context.Background(), mkInv("2026-05-19T14:30:00.000Z")); err != nil {
			t.Fatalf("first Dispatch: %v", err)
		}
		// Same instant, different representation — must hit the cache.
		if _, err := disp.Dispatch(context.Background(), mkInv("2026-05-19T20:00:00+05:30")); err != nil {
			t.Fatalf("second Dispatch: %v", err)
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("adapter.calls = %d; want 1 (Rule 4 normalization must collapse equivalent instants)", got)
		}
	})

	// Sub-test: normalization OFF → second equivalent-instant call misses
	// (the documented Rule 3 cache-miss class).
	t.Run("normalize_off_keeps_verbatim_keys", func(t *testing.T) {
		var calls atomic.Int32
		adapter := AdapterFunc(func(_ context.Context, _ *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			calls.Add(1)
			return &Response{Body: []byte(`{"items":[]}`), Format: "json", StatusCode: 200}, nil
		})
		mc := cache.NewMemCache(100, time.Minute)
		disp := NewDispatcherWithConfig(mkCatalog(), map[string]Adapter{adapterKey: adapter},
			DispatcherConfig{Cache: mc})

		if _, err := disp.Dispatch(context.Background(), mkInv("2026-05-19T14:30:00.000Z")); err != nil {
			t.Fatalf("first Dispatch: %v", err)
		}
		if _, err := disp.Dispatch(context.Background(), mkInv("2026-05-19T20:00:00+05:30")); err != nil {
			t.Fatalf("second Dispatch: %v", err)
		}
		if got := calls.Load(); got != 2 {
			t.Errorf("adapter.calls = %d; want 2 (Rule 3 verbatim must miss on representation differences)", got)
		}
	})

	// Sub-test: §2028. A 304 answer returns the validator and nothing else —
	// no shaped body, no _expression envelope, no tee artifact.
	t.Run("304_short_circuits_the_expression_pipeline", func(t *testing.T) {
		fx := newETagFixture(t)

		first, err := fx.dispatch.Dispatch(context.Background(), etagInvocation("v1", "fp-a"))
		if err != nil {
			t.Fatalf("first Dispatch: %v", err)
		}
		if first.Expression == nil {
			t.Fatal("first response has no _expression; the cold path must shape normally")
		}
		teeAfterFirst := fx.teeFileCount(t)
		if teeAfterFirst == 0 {
			t.Fatal("cold call wrote no tee artifact; the 304 comparison below proves nothing")
		}

		second, err := fx.dispatch.Dispatch(context.Background(), etagInvocation("v1", "fp-a"))
		if err != nil {
			t.Fatalf("replay Dispatch: %v", err)
		}
		if got := fx.conditional.Load(); got != 1 {
			t.Fatalf("conditional requests = %d; want 1 (the replay must carry If-None-Match)", got)
		}
		if got := string(second.Body); got != `{"unchanged":true,"etag":"W/\"v1-abc\""}` {
			t.Errorf("304 body = %s; want the §2029 two-key object", got)
		}
		if second.Expression != nil {
			t.Errorf("_expression = %+v; want nil (§2029 omits it from 304 responses)", second.Expression)
		}
		if second.FullResultPath != "" || second.FullResultResource != "" {
			t.Errorf("recovery handles = %q/%q; want empty (§2028 mints none)", second.FullResultPath, second.FullResultResource)
		}
		if got := fx.teeFileCount(t); got != teeAfterFirst {
			t.Errorf("tee files = %d; want %d unchanged (§2028 writes no artifact on 304)", got, teeAfterFirst)
		}
		var structured map[string]any
		if err := json.Unmarshal(second.Body, &structured); err != nil {
			t.Fatalf("304 body not JSON: %v", err)
		}
		if structured["unchanged"] != true || structured["etag"] != etagValidator {
			t.Errorf("304 body = %v; want unchanged=true and the stored validator", structured)
		}
	})

	// Sub-test: §2030. The row is a positive saving, so its baseline must be
	// the body the caller did not receive.
	t.Run("ledger_records_etag_304_with_zero_response_tokens", func(t *testing.T) {
		fx := newETagFixture(t)

		if _, err := fx.dispatch.Dispatch(context.Background(), etagInvocation("v1", "fp-a")); err != nil {
			t.Fatalf("first Dispatch: %v", err)
		}
		if _, err := fx.dispatch.Dispatch(context.Background(), etagInvocation("v1", "fp-a")); err != nil {
			t.Fatalf("replay Dispatch: %v", err)
		}
		if len(fx.ledger.entries) != 2 {
			t.Fatalf("ledger entries = %d; want 2", len(fx.ledger.entries))
		}
		e := fx.ledger.entries[1]
		if e.CacheStatus != "etag_304" {
			t.Errorf("cache_status = %q; want %q", e.CacheStatus, "etag_304")
		}
		if !e.ServedFromCache {
			t.Error("served_from_cache = false; the body came from the cache, not the wire")
		}
		if e.ResponseTokens != 0 {
			t.Errorf("response_tokens = %d; want 0", e.ResponseTokens)
		}
		if e.RawTokens == 0 {
			t.Error("raw_tokens = 0; the cached body is the baseline the oracle would have paid")
		}
		if e.RawTokens <= e.ShapedTokens {
			t.Errorf("raw_tokens=%d shaped_tokens=%d; a 304 must contribute a positive saving", e.RawTokens, e.ShapedTokens)
		}
		if cold := fx.ledger.entries[0]; e.RequestTokens <= cold.RequestTokens {
			t.Errorf("request_tokens = %d; want more than the unconditional %d (the If-None-Match header counts)", e.RequestTokens, cold.RequestTokens)
		}
	})

	// Sub-test: row 67's cross-variant clause. variant_id is a key component,
	// so the stored validator must not travel to another variant.
	t.Run("a_different_variant_does_not_revalidate", func(t *testing.T) {
		fx := newETagFixture(t)

		if _, err := fx.dispatch.Dispatch(context.Background(), etagInvocation("v1", "fp-a")); err != nil {
			t.Fatalf("first Dispatch: %v", err)
		}
		if _, err := fx.dispatch.Dispatch(context.Background(), etagInvocation("v2", "fp-a")); err != nil {
			t.Fatalf("second Dispatch: %v", err)
		}
		if got := fx.conditional.Load(); got != 0 {
			t.Errorf("conditional requests = %d; want 0 (a different variant is a different key)", got)
		}
		if got := fx.validators; len(got) != 2 || got[1] != "" {
			t.Errorf("validators = %q; want the second call to carry none", got)
		}
	})

	// Sub-test: row 67's cross-principal clause. §10.0.1's fingerprint is a key
	// component, so one principal's validator cannot answer for another.
	t.Run("a_different_principal_does_not_revalidate", func(t *testing.T) {
		fx := newETagFixture(t)

		if _, err := fx.dispatch.Dispatch(context.Background(), etagInvocation("v1", "fp-a")); err != nil {
			t.Fatalf("first Dispatch: %v", err)
		}
		if _, err := fx.dispatch.Dispatch(context.Background(), etagInvocation("v1", "fp-b")); err != nil {
			t.Fatalf("second Dispatch: %v", err)
		}
		if got := fx.conditional.Load(); got != 0 {
			t.Errorf("conditional requests = %d; want 0 (a different principal is a different key)", got)
		}
	})

	// Sub-test: §2031 item 4. The cache key carries no profile component, so a
	// caller who changed profiles still gets the 304 and must clear the entry
	// to re-shape the resource.
	t.Run("a_profile_change_does_not_affect_the_304", func(t *testing.T) {
		fx := newETagFixture(t)

		if _, err := fx.dispatch.Dispatch(context.Background(), etagInvocation("v1", "fp-a")); err != nil {
			t.Fatalf("first Dispatch: %v", err)
		}
		reshaped := etagInvocation("v1", "fp-a")
		reshaped.OutputProfile = &profile.Profile{Recovery: "none"}
		second, err := fx.dispatch.Dispatch(context.Background(), reshaped)
		if err != nil {
			t.Fatalf("replay Dispatch: %v", err)
		}
		if got := fx.conditional.Load(); got != 1 {
			t.Fatalf("conditional requests = %d; want 1", got)
		}
		if second.Expression != nil {
			t.Errorf("_expression = %+v; want nil under the new profile too", second.Expression)
		}
	})
}

// TestDualFetchDropsTheMaskedValidator pins the boundary between §10.2 and
// §9.1: the stored validator is keyed to args_canonical, and the recovery
// fetch deletes `fields` from those args. Sending the masked request's
// validator on the unmasked request invites a bodiless 304, and stage 9 would
// then write a full_result_path artifact holding nothing.
//
// The adapter answers a masked request with 200 even when it carries a
// validator (upstream says the resource changed), which is what lets the
// dispatch reach step 7a2 instead of short-circuiting at 7a1.
func TestDualFetchDropsTheMaskedValidator(t *testing.T) {
	const maskedETag = `W/"masked-1"`

	type call struct {
		mask      string
		validator string
	}
	var mu sync.Mutex
	var calls []call

	adapter := &funcAdapter{
		execute: func(_ context.Context, inv *Invocation, _ *ResolvedVariant, _ *Credentials) (*Response, error) {
			mask, _ := inv.Args[dualFetchArg].(string)
			mu.Lock()
			calls = append(calls, call{mask: mask, validator: inv.IfNoneMatch})
			mu.Unlock()
			if mask == "" {
				if inv.IfNoneMatch != "" {
					// The failure the fix prevents: a 304 on the recovery
					// request leaves the artifact with no body to hold.
					return &Response{Format: "json", StatusCode: http.StatusNotModified, ETag: inv.IfNoneMatch}, nil
				}
				return &Response{Body: []byte(dualUnmasked), Format: "json", StatusCode: http.StatusOK}, nil
			}
			return &Response{Body: []byte(dualMasked), Format: "json", StatusCode: http.StatusOK, ETag: maskedETag}, nil
		},
	}

	disp := NewDispatcherWithConfig(dualCatalog("stub"), map[string]Adapter{"stub": adapter},
		DispatcherConfig{
			HTTPCache: cache.NewHTTPCache(newMemHTTPStore()),
			Tee:       TeeConfig{ProfileDir: t.TempDir(), Mode: "always", RetentionHours: 24},
		})

	if _, err := disp.Dispatch(context.Background(), dualInvocation(dualMask)); err != nil {
		t.Fatalf("cold Dispatch: %v", err)
	}
	res, err := disp.Dispatch(context.Background(), dualInvocation(dualMask))
	if err != nil {
		t.Fatalf("warm Dispatch: %v", err)
	}

	mu.Lock()
	got := append([]call(nil), calls...)
	mu.Unlock()
	if len(got) != 4 {
		t.Fatalf("adapter calls = %d (%+v); want 4 (two dispatches, masked + unmasked each)", len(got), got)
	}
	if got[2].validator != maskedETag {
		t.Fatalf("warm masked request validator = %q; want %q (the §10.2 store never armed, so this test proves nothing)", got[2].validator, maskedETag)
	}
	if got[3].validator != "" {
		t.Errorf("recovery request validator = %q; want empty: the validator belongs to the masked args", got[3].validator)
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
}
