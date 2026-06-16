// Spec §10.3 acceptance for the semantic response cache: 5-component key
// shape, per-op TTL lookup, VAAC eviction priority, and principal scoping
// (auth_subject_fingerprint dimension). The dispatcher-level acceptance
// ("gum.cache_stats returns non-zero semantic.hits after repeated identical
// calls") is covered in internal/dispatch/semantic_cache_test.go.

package cache

import (
	"testing"
	"time"
)

// TestSemanticKeyComponentsAreDistinguished asserts that flipping any of
// the five components produces a fresh key. This is the principal-scoping
// guarantee from spec §10.3 + §10.0.1.
func TestSemanticKeyComponentsAreDistinguished(t *testing.T) {
	base := SemanticKey("op", "variant", "args", "fields", "fp")
	cases := []struct {
		name string
		got  string
	}{
		{"op", SemanticKey("OP", "variant", "args", "fields", "fp")},
		{"variant", SemanticKey("op", "VARIANT", "args", "fields", "fp")},
		{"args", SemanticKey("op", "variant", "ARGS", "fields", "fp")},
		{"fields", SemanticKey("op", "variant", "args", "FIELDS", "fp")},
		{"fp", SemanticKey("op", "variant", "args", "fields", "FP")},
	}
	for _, c := range cases {
		if c.got == base {
			t.Errorf("flipping %s did not change key", c.name)
		}
	}
}

// TestSemanticKeyDeterministic asserts the hash is stable across calls.
func TestSemanticKeyDeterministic(t *testing.T) {
	a := SemanticKey("op", "v", "args", "f", "fp")
	b := SemanticKey("op", "v", "args", "f", "fp")
	if a != b {
		t.Errorf("SemanticKey non-deterministic: %q vs %q", a, b)
	}
}

// TestSemanticCacheGetMissThenHit confirms the basic Set/Get pair drives
// hit/miss counters as expected.
func TestSemanticCacheGetMissThenHit(t *testing.T) {
	c := NewSemanticCache(SemanticConfig{MaxEntries: 4})
	if _, ok := c.Get("k"); ok {
		t.Fatal("Get on empty cache returned ok=true")
	}
	if s := c.Stats(); s.Misses != 1 {
		t.Errorf("misses=%d after one miss; want 1", s.Misses)
	}
	c.Set("k", []byte("payload"), "any.op")
	got, ok := c.Get("k")
	if !ok || string(got) != "payload" {
		t.Errorf("Get post-Set = (%q, %v); want (payload, true)", got, ok)
	}
	if s := c.Stats(); s.Hits != 1 {
		t.Errorf("hits=%d after one hit; want 1", s.Hits)
	}
}

// TestSemanticCachePerOpTTL: an entry stored under an op with a known
// short TTL expires before one stored under an op with a long TTL.
func TestSemanticCachePerOpTTL(t *testing.T) {
	cfg := SemanticConfig{
		MaxEntries: 8,
		DefaultTTL: time.Hour,
		PerOpTTL: map[string]time.Duration{
			"short.op": 10 * time.Millisecond,
		},
	}
	c := NewSemanticCache(cfg)
	c.Set("short", []byte("x"), "short.op")
	c.Set("long", []byte("y"), "long.op")
	time.Sleep(40 * time.Millisecond)
	if _, ok := c.Get("short"); ok {
		t.Error("short.op entry survived past its 10ms TTL")
	}
	if _, ok := c.Get("long"); !ok {
		t.Error("long.op entry expired before its 1h TTL")
	}
}

// TestSemanticCacheTTLForOpLookup: TTLForOp honors the per-op table and
// falls back to the configured default.
func TestSemanticCacheTTLForOpLookup(t *testing.T) {
	c := NewSemanticCache(SemanticConfig{
		DefaultTTL: 5 * time.Second,
		PerOpTTL:   map[string]time.Duration{"a.op": 42 * time.Second},
	})
	if got := c.TTLForOp("a.op"); got != 42*time.Second {
		t.Errorf("TTLForOp(a.op) = %v; want 42s", got)
	}
	if got := c.TTLForOp("missing.op"); got != 5*time.Second {
		t.Errorf("TTLForOp(missing.op) = %v; want default 5s", got)
	}
}

// TestSemanticCacheVAACEviction: when MaxEntries is exceeded, the entry
// with the lowest VAAC score is evicted first. A hot key (multiple hits)
// must survive eviction even when a cold key was inserted later.
func TestSemanticCacheVAACEviction(t *testing.T) {
	c := NewSemanticCache(SemanticConfig{MaxEntries: 2, DefaultTTL: time.Hour})
	c.Set("hot", []byte("payload-hot"), "any.op")
	c.Set("cold", []byte("payload-cold"), "any.op")
	// Heat up "hot" so it has frequency > 1.
	for i := 0; i < 5; i++ {
		c.Get("hot")
	}
	// Adding a third entry triggers eviction. Cold should lose because hot
	// has higher frequency (5 hits vs 0).
	c.Set("third", []byte("payload-third"), "any.op")
	if _, ok := c.Get("hot"); !ok {
		t.Error("hot key evicted despite higher VAAC score")
	}
	if _, ok := c.Get("cold"); ok {
		t.Error("cold key survived despite lower VAAC score")
	}
	if s := c.Stats(); s.Evictions == 0 {
		t.Error("Evictions counter did not increment on cap overflow")
	}
}

// TestSemanticCacheBytesAndLen pins the size accounting methods.
func TestSemanticCacheBytesAndLen(t *testing.T) {
	c := NewSemanticCache(SemanticConfig{MaxEntries: 4})
	c.Set("a", []byte("hello"), "op")
	c.Set("b", []byte("world!"), "op")
	if got := c.Len(); got != 2 {
		t.Errorf("Len=%d; want 2", got)
	}
	if got := c.Bytes(); got != int64(len("hello")+len("world!")) {
		t.Errorf("Bytes=%d; want %d", got, len("hello")+len("world!"))
	}
}

// TestSemanticCacheReplacePreservesKey: re-Set of an existing key updates
// the value but does not double-count the entry. (Implementation also
// preserves hit count internally to avoid losing VAAC standing.)
func TestSemanticCacheReplacePreservesKey(t *testing.T) {
	c := NewSemanticCache(SemanticConfig{MaxEntries: 4, DefaultTTL: time.Hour})
	c.Set("k", []byte("v1"), "op")
	c.Set("k", []byte("v2"), "op")
	if got := c.Len(); got != 1 {
		t.Errorf("Len=%d after replace; want 1", got)
	}
	if v, ok := c.Get("k"); !ok || string(v) != "v2" {
		t.Errorf("Get after replace = (%q, %v); want (v2, true)", v, ok)
	}
}

// TestPerOpTTLTableShape sanity-checks the spec §10.3 line 2254 table
// includes the spec-listed entries with the documented values.
func TestPerOpTTLTableShape(t *testing.T) {
	// calendar.events: 60s; drive.files.list: 300s; gmail.profiles.get: 3600s.
	mustHave := map[string]time.Duration{
		"calendar.events.list":   60 * time.Second,
		"drive.files.list":       300 * time.Second,
		"gmail.profiles.get":     3600 * time.Second,
	}
	for k, want := range mustHave {
		got, ok := PerOpTTL[k]
		if !ok {
			t.Errorf("PerOpTTL missing entry for %q", k)
			continue
		}
		if got != want {
			t.Errorf("PerOpTTL[%q] = %v; want %v", k, got, want)
		}
	}
}
