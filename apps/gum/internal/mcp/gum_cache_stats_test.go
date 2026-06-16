// Package mcp — Red Team failing tests for gum-9vuq.8.
//
// Covers: gum.cache_stats handler envelope shape (4 required top-level keys),
// semantic sub-object shape (5 fields), live cache hit/miss wiring,
// http/prompt sub-object shapes, audit_broken presence, no extra keys.
//
// Spec anchors:
//   - spec.md §3003 CacheStatsResult wire shape (4 required top-level keys,
//     additionalProperties:false at root).
//   - spec.md §2335-2336: audit_broken sentinel (v0.1.0: false).
//
// These tests MUST FAIL today because handleCacheStats returns a flat map with
// {version, hits, misses, entries, bytes, note} — missing semantic/http/prompt
// nesting and missing audit_broken.  Test 3 (live hits) additionally requires
// the Dispatcher interface to expose CacheStats(), which does not exist yet.
package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/cache"
	"github.com/ehmo/gum/internal/dispatch"
)

// isolateAuditSentinel redirects XDG_DATA_HOME to a fresh tempdir so the
// audit.broken probe in handleCacheStats cannot see a real sentinel on the
// developer machine. Call at the top of any test that invokes cache_stats but
// does not itself seed an audit.broken file.
func isolateAuditSentinel(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// ---------------------------------------------------------------------------
// Test-local types
// ---------------------------------------------------------------------------

// noopDispatcher is a minimal dispatch.Dispatcher stub for shape-only tests.
type noopDispatcher struct{}

func (noopDispatcher) Dispatch(_ context.Context, _ *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return &dispatch.ShapedResponse{Body: []byte(`{}`)}, nil
}

func (noopDispatcher) CacheStats() dispatch.CacheLayerStats {
	return dispatch.CacheLayerStats{}
}

// CacheLayerStats is an alias for dispatch.CacheLayerStats used by Test 3's
// local cacheStatProvider interface assertion. Green has shipped the real type.
type CacheLayerStats = dispatch.CacheLayerStats

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeCacheStatsRequest returns a no-arg CallToolRequest for gum.cache_stats.
func makeCacheStatsRequest() *sdkmcp.CallToolRequest {
	return &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name:      "gum.cache_stats",
			Arguments: json.RawMessage(`{}`),
		},
	}
}

// invokeCacheStats calls handleCacheStats on srv and returns the parsed top-level
// JSON map, failing the test on any error. Callers that depend on the
// audit.broken sentinel probe (§2333-2336) MUST redirect XDG_DATA_HOME to a
// tempdir BEFORE calling this helper.
func invokeCacheStats(t *testing.T, srv *Server) map[string]any {
	t.Helper()
	res, err := srv.handleCacheStats(context.Background(), makeCacheStatsRequest())
	if err != nil {
		t.Fatalf("handleCacheStats returned Go error: %v", err)
	}
	if res == nil {
		t.Fatal("handleCacheStats returned nil result")
	}
	if res.IsError {
		var text string
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
				text = tc.Text
			}
		}
		t.Fatalf("handleCacheStats returned error result: %s", text)
	}
	if len(res.Content) == 0 {
		t.Fatal("handleCacheStats returned empty content")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is not TextContent; got %T", res.Content[0])
	}
	var m map[string]any
	if jerr := json.Unmarshal([]byte(tc.Text), &m); jerr != nil {
		t.Fatalf("handleCacheStats result is not JSON: %v; text: %s", jerr, tc.Text)
	}
	return m
}

// assertIntField asserts that m[key] is a JSON number that represents a non-negative integer.
func assertIntField(t *testing.T, m map[string]any, key string) {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Errorf("missing field %q", key)
		return
	}
	n, ok := v.(float64) // JSON numbers unmarshal as float64
	if !ok {
		t.Errorf("field %q is %T; want number", key, v)
		return
	}
	if n < 0 {
		t.Errorf("field %q = %v; want >= 0", key, n)
	}
}

// assertExactKeys asserts that m has exactly the keys in want (no more, no fewer).
func assertExactKeys(t *testing.T, m map[string]any, want []string, context string) {
	t.Helper()
	wantSet := make(map[string]bool, len(want))
	for _, k := range want {
		wantSet[k] = true
	}
	for _, k := range want {
		if _, ok := m[k]; !ok {
			t.Errorf("%s: missing key %q", context, k)
		}
	}
	for k := range m {
		if !wantSet[k] {
			t.Errorf("%s: unexpected extra key %q", context, k)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 1: envelope has exactly {semantic, http, prompt, audit_broken}
// ---------------------------------------------------------------------------

// TestCacheStatsEnvelopeHasAllFourTopLevelKeys invokes the handler with no args
// and asserts the top-level JSON object has exactly 4 keys:
// semantic, http, prompt, audit_broken.
//
// Current handler returns {version, hits, misses, entries, bytes, note} — 6 wrong
// keys, 0 correct keys.  MUST FAIL until the handler is rewritten.
func TestCacheStatsEnvelopeHasAllFourTopLevelKeys(t *testing.T) {
	isolateAuditSentinel(t)
	srv := NewServer(noopDispatcher{})
	m := invokeCacheStats(t, srv)

	want := []string{"semantic", "http", "prompt", "audit_broken"}
	assertExactKeys(t, m, want, "top-level")
}

// ---------------------------------------------------------------------------
// Test 2: semantic sub-object shape
// ---------------------------------------------------------------------------

// TestCacheStatsSemanticShape asserts that result["semantic"] is an object
// with exactly {hits, misses, evictions, entries, bytes}, all non-negative integers.
//
// Current handler has no "semantic" key at all.  MUST FAIL.
func TestCacheStatsSemanticShape(t *testing.T) {
	isolateAuditSentinel(t)
	srv := NewServer(noopDispatcher{})
	m := invokeCacheStats(t, srv)

	raw, ok := m["semantic"]
	if !ok {
		t.Fatal("top-level \"semantic\" key missing")
	}
	semantic, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("\"semantic\" is %T; want object", raw)
	}

	wantKeys := []string{"hits", "misses", "evictions", "entries", "bytes"}
	assertExactKeys(t, semantic, wantKeys, "semantic")
	for _, k := range wantKeys {
		assertIntField(t, semantic, k)
	}
}

// ---------------------------------------------------------------------------
// Test 3: live hit/miss wiring through the dispatcher
// ---------------------------------------------------------------------------

// TestCacheStatsSemanticLiveHits builds a Server whose dispatcher holds a
// *cache.MemCache, performs 1 cache hit and 1 cache miss, then asserts
// semantic.hits >= 1 and semantic.misses >= 1.
//
// This is the LIVE WIRING test.  It will fail for two reasons today:
//  1. dispatch.CacheLayerStats does not exist.
//  2. The Dispatcher interface has no CacheStats() method.
//  3. handleCacheStats does not call d.disp.CacheStats().
//
// If the Server constructor does not yet accept a dispatcher that exposes
// CacheStats, this test is skipped with a clear message for Green.
func TestCacheStatsSemanticLiveHits(t *testing.T) {
	isolateAuditSentinel(t)
	// Build a real MemCache, prime it with one hit and one miss.
	mc := cache.NewMemCache(10, time.Minute)
	key := cache.KeyFor("test.op", "{}", "", "cred1")
	mc.Set(key, []byte(`{"ok":true}`))
	_, _ = mc.Get(key)                              // hit
	_, _ = mc.Get("does-not-exist-in-cache-at-all") // miss

	stats := mc.Stats()
	if stats.Hits < 1 {
		t.Fatalf("pre-condition: expected >=1 hit after Set+Get; got %d", stats.Hits)
	}
	if stats.Misses < 1 {
		t.Fatalf("pre-condition: expected >=1 miss after absent Get; got %d", stats.Misses)
	}

	// Build a dispatcher wired to this MemCache.
	// NewDispatcherWithConfig wires cfg.Cache into the dispatcher's cache field.
	d := dispatch.NewDispatcherWithConfig(nil, nil, dispatch.DispatcherConfig{
		Cache: mc,
	})

	// Assert the dispatcher exposes CacheStats() via the cacheStatProvider seam
	// (concrete method on *dispatcher; not part of the Dispatcher interface).
	type cacheStatProvider interface {
		CacheStats() CacheLayerStats
	}
	cs, ok := d.(cacheStatProvider)
	if !ok {
		t.Fatal("Dispatcher does not implement CacheStats() — Green implementation missing")
	}

	srv := NewServer(d)
	m := invokeCacheStats(t, srv)

	raw, ok := m["semantic"]
	if !ok {
		t.Fatal("\"semantic\" key missing from response")
	}
	semantic, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("\"semantic\" is %T; want object", raw)
	}

	hits, _ := semantic["hits"].(float64)
	misses, _ := semantic["misses"].(float64)

	if hits < 1 {
		t.Errorf("semantic.hits = %v; want >= 1 (live wiring: cache had %d hits)", hits, cs.CacheStats().Hits)
	}
	if misses < 1 {
		t.Errorf("semantic.misses = %v; want >= 1 (live wiring: cache had %d misses)", misses, cs.CacheStats().Misses)
	}
}

// ---------------------------------------------------------------------------
// Test 4: http sub-object shape
// ---------------------------------------------------------------------------

// TestCacheStatsHttpShape asserts result["http"] has exactly {hits, misses, entries, bytes},
// all non-negative integers.  v0.1.0 values may all be zero (no HTTP cache), but
// KEYS must be present per spec §3003.
//
// Current handler has no "http" key.  MUST FAIL.
func TestCacheStatsHttpShape(t *testing.T) {
	isolateAuditSentinel(t)
	srv := NewServer(noopDispatcher{})
	m := invokeCacheStats(t, srv)

	raw, ok := m["http"]
	if !ok {
		t.Fatal("top-level \"http\" key missing")
	}
	http, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("\"http\" is %T; want object", raw)
	}

	wantKeys := []string{"hits", "misses", "entries", "bytes"}
	assertExactKeys(t, http, wantKeys, "http")
	for _, k := range wantKeys {
		assertIntField(t, http, k)
	}
}

// ---------------------------------------------------------------------------
// Test 5: prompt sub-object shape
// ---------------------------------------------------------------------------

// TestCacheStatsPromptShape asserts result["prompt"] has exactly {supported, hits_estimate}.
// supported must be bool (= false for v0.1.0).
// hits_estimate must be integer or null (= null for v0.1.0).
//
// Current handler has no "prompt" key.  MUST FAIL.
func TestCacheStatsPromptShape(t *testing.T) {
	isolateAuditSentinel(t)
	srv := NewServer(noopDispatcher{})
	m := invokeCacheStats(t, srv)

	raw, ok := m["prompt"]
	if !ok {
		t.Fatal("top-level \"prompt\" key missing")
	}
	prompt, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("\"prompt\" is %T; want object", raw)
	}

	// Exactly 2 keys.
	wantKeys := []string{"supported", "hits_estimate"}
	assertExactKeys(t, prompt, wantKeys, "prompt")

	// supported must be bool (v0.1.0 = false).
	supRaw, hasSup := prompt["supported"]
	if !hasSup {
		t.Error("prompt.supported missing")
	} else {
		if _, ok := supRaw.(bool); !ok {
			t.Errorf("prompt.supported is %T; want bool", supRaw)
		}
	}

	// hits_estimate must be float64 (number) or nil (JSON null).
	heRaw, hasHE := prompt["hits_estimate"]
	if !hasHE {
		t.Error("prompt.hits_estimate missing")
	} else if heRaw != nil {
		// If not null, must be a non-negative number.
		n, ok := heRaw.(float64)
		if !ok {
			t.Errorf("prompt.hits_estimate is %T; want number or null", heRaw)
		} else if n < 0 {
			t.Errorf("prompt.hits_estimate = %v; want >= 0 or null", n)
		}
	}
	// null (nil) is explicitly allowed per spec §3003.
}

// ---------------------------------------------------------------------------
// Test 6: audit_broken is present and false
// ---------------------------------------------------------------------------

// TestCacheStatsAuditBrokenPresent asserts result["audit_broken"] is a bool
// equal to false for v0.1.0 (no sentinel implementation per spec §2335).
//
// Current handler has no "audit_broken" key.  MUST FAIL.
func TestCacheStatsAuditBrokenPresent(t *testing.T) {
	isolateAuditSentinel(t)
	srv := NewServer(noopDispatcher{})
	m := invokeCacheStats(t, srv)

	raw, ok := m["audit_broken"]
	if !ok {
		t.Fatal("top-level \"audit_broken\" key missing (spec §2335)")
	}
	ab, ok := raw.(bool)
	if !ok {
		t.Fatalf("audit_broken is %T; want bool", raw)
	}
	if ab {
		t.Error("audit_broken = true; want false for v0.1.0 (sentinel not implemented)")
	}
}

// ---------------------------------------------------------------------------
// Test 7: no extra top-level keys
// ---------------------------------------------------------------------------

// TestCacheStatsNoExtraKeys asserts the top-level envelope has ONLY the four
// spec-mandated keys: semantic, http, prompt, audit_broken.
// No version, no note, no hits, no _expression (optional per §3043, excluded
// until mandated).
//
// Current handler emits version+hits+misses+entries+bytes+note — 6 extra keys.  MUST FAIL.
func TestCacheStatsNoExtraKeys(t *testing.T) {
	isolateAuditSentinel(t)
	srv := NewServer(noopDispatcher{})
	m := invokeCacheStats(t, srv)

	allowed := map[string]bool{
		"semantic":     true,
		"http":         true,
		"prompt":       true,
		"audit_broken": true,
	}
	for k := range m {
		if !allowed[k] {
			t.Errorf("unexpected top-level key %q (spec §3003: additionalProperties:false at root)", k)
		}
	}
}
