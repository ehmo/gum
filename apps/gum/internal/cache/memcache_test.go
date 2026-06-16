package cache_test

import (
	"fmt"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/cache"
)

// catchPanic calls fn and returns ("panic: not implemented", true) if fn panics,
// or ("", false) otherwise. Used to detect unimplemented stubs without killing
// the test binary.
func catchPanic(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v", r)
			panicked = true
		}
	}()
	fn()
	return "", false
}

// TestMemCacheSetOverwritesExistingKey pins the "update existing entry"
// branch: a second Set under the same key must replace the value in
// place without bumping evictions. A regression that fell through to
// PushFront would double-insert and silently corrupt LRU bookkeeping.
func TestMemCacheSetOverwritesExistingKey(t *testing.T) {
	c := cache.NewMemCache(4, 0)
	c.Set("k", []byte("v1"))
	c.Set("k", []byte("v2"))
	got, ok := c.Get("k")
	if !ok || string(got) != "v2" {
		t.Errorf("Get after overwrite: ok=%v val=%q; want true 'v2'", ok, got)
	}
	stats := c.Stats()
	if stats.Evictions != 0 {
		t.Errorf("evictions=%d; want 0 after overwrite", stats.Evictions)
	}
}

// TestMemCacheRoundTrip verifies basic Set + Get behaviour.
func TestMemCacheRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)

	var c *cache.MemCache
	msg, panicked := catchPanic(func() {
		c = cache.NewMemCache(10, time.Minute)
	})
	if panicked {
		t.Fatalf("NewMemCache panicked: %s — green team must implement NewMemCache", msg)
	}

	key := cache.KeyFor("gmail.users.messages.list", `{"userId":"me"}`, "scope-hash-1", "creds-id-1")

	// Miss before Set.
	var ok bool
	msg, panicked = catchPanic(func() {
		_, ok = c.Get(key)
	})
	if panicked {
		t.Fatalf("MemCache.Get panicked: %s", msg)
	}
	if ok {
		t.Fatal("Get before Set returned ok=true; expected cache miss")
	}

	payload := []byte(`{"messages":[{"id":"abc"}]}`)
	msg, panicked = catchPanic(func() {
		c.Set(key, payload)
	})
	if panicked {
		t.Fatalf("MemCache.Set panicked: %s", msg)
	}

	var got []byte
	msg, panicked = catchPanic(func() {
		got, ok = c.Get(key)
	})
	if panicked {
		t.Fatalf("MemCache.Get panicked: %s", msg)
	}
	if !ok {
		t.Fatal("Get after Set returned ok=false; expected cache hit")
	}
	if string(got) != string(payload) {
		t.Errorf("Get = %q, want %q", got, payload)
	}

	var stats cache.Stats
	msg, panicked = catchPanic(func() {
		stats = c.Stats()
	})
	if panicked {
		t.Fatalf("MemCache.Stats panicked: %s", msg)
	}
	if stats.Hits != 1 {
		t.Errorf("Stats.Hits = %d, want 1", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Stats.Misses = %d, want 1", stats.Misses)
	}
}

// TestMemCacheLRUEviction verifies that with maxEntries=2, inserting a third
// entry evicts the LRU entry and the eviction counter increments.
func TestMemCacheLRUEviction(t *testing.T) {
	defer goleak.VerifyNone(t)

	var c *cache.MemCache
	msg, panicked := catchPanic(func() {
		c = cache.NewMemCache(2, time.Minute)
	})
	if panicked {
		t.Fatalf("NewMemCache panicked: %s", msg)
	}

	key1 := cache.KeyFor("op1", "args1", "scopes1", "creds1")
	key2 := cache.KeyFor("op2", "args2", "scopes2", "creds2")
	key3 := cache.KeyFor("op3", "args3", "scopes3", "creds3")

	msg, panicked = catchPanic(func() {
		c.Set(key1, []byte("v1"))
		c.Set(key2, []byte("v2"))
		c.Get(key1) // access key1 to make key2 the LRU
		c.Set(key3, []byte("v3")) // evicts key2
	})
	if panicked {
		t.Fatalf("cache operations panicked: %s", msg)
	}

	var ok bool
	// key1 and key3 must be present.
	msg, panicked = catchPanic(func() {
		_, ok = c.Get(key1)
	})
	if panicked {
		t.Fatalf("Get(key1) panicked: %s", msg)
	}
	if !ok {
		t.Error("key1 should still be in cache after LRU eviction of key2")
	}

	msg, panicked = catchPanic(func() {
		_, ok = c.Get(key3)
	})
	if panicked {
		t.Fatalf("Get(key3) panicked: %s", msg)
	}
	if !ok {
		t.Error("key3 should be in cache (just inserted)")
	}

	// key2 must have been evicted.
	msg, panicked = catchPanic(func() {
		_, ok = c.Get(key2)
	})
	if panicked {
		t.Fatalf("Get(key2) panicked: %s", msg)
	}
	if ok {
		t.Error("key2 should have been evicted (LRU), but Get returned ok=true")
	}

	var stats cache.Stats
	msg, panicked = catchPanic(func() {
		stats = c.Stats()
	})
	if panicked {
		t.Fatalf("Stats panicked: %s", msg)
	}
	if stats.Evictions < 1 {
		t.Errorf("Stats.Evictions = %d, want >=1", stats.Evictions)
	}
}

// TestMemCacheTTLExpiry verifies that entries expire after the configured TTL.
func TestMemCacheTTLExpiry(t *testing.T) {
	defer goleak.VerifyNone(t)

	const ttl = 50 * time.Millisecond
	var c *cache.MemCache
	msg, panicked := catchPanic(func() {
		c = cache.NewMemCache(10, ttl)
	})
	if panicked {
		t.Fatalf("NewMemCache panicked: %s", msg)
	}

	key := cache.KeyFor("op", "args", "scopes", "creds")

	msg, panicked = catchPanic(func() {
		c.Set(key, []byte("fresh"))
	})
	if panicked {
		t.Fatalf("Set panicked: %s", msg)
	}

	var ok bool
	msg, panicked = catchPanic(func() {
		_, ok = c.Get(key)
	})
	if panicked {
		t.Fatalf("Get (before expiry) panicked: %s", msg)
	}
	if !ok {
		t.Fatal("Get immediately after Set returned miss; expected hit")
	}

	time.Sleep(ttl + 20*time.Millisecond)

	msg, panicked = catchPanic(func() {
		_, ok = c.Get(key)
	})
	if panicked {
		t.Fatalf("Get (after expiry) panicked: %s", msg)
	}
	if ok {
		t.Error("Get after TTL expiry returned ok=true; expected miss")
	}

	var stats cache.Stats
	msg, panicked = catchPanic(func() {
		stats = c.Stats()
	})
	if panicked {
		t.Fatalf("Stats panicked: %s", msg)
	}
	if stats.Misses < 1 {
		t.Errorf("Stats.Misses = %d after TTL expiry, want >=1", stats.Misses)
	}
}

// TestKeyForDeterminism verifies that KeyFor is deterministic and that different
// inputs produce different keys.
func TestKeyForDeterminism(t *testing.T) {
	defer goleak.VerifyNone(t)

	// KeyFor is implemented (no panic body — sha256 is in the signature file).
	k1a := cache.KeyFor("op1", "args1", "s1", "c1")
	k1b := cache.KeyFor("op1", "args1", "s1", "c1")
	if k1a != k1b {
		t.Errorf("KeyFor is not deterministic: %q != %q", k1a, k1b)
	}

	keys := make(map[string]string)
	for i := range 4 {
		k := cache.KeyFor(fmt.Sprintf("op%d", i), "args", "scopes", "creds")
		if existing, seen := keys[k]; seen {
			t.Errorf("collision: op%d and %s produced the same key %q", i, existing, k)
		}
		keys[k] = fmt.Sprintf("op%d", i)
	}
}

// TestMemCacheLenAndBytes exercises the two introspection helpers.
//   - Empty cache → Len=0, Bytes=0.
//   - After inserts, Len matches the count and Bytes equals the sum of
//     value lengths (the metadata overhead is intentionally ignored).
func TestMemCacheLenAndBytes(t *testing.T) {
	defer goleak.VerifyNone(t)
	c := cache.NewMemCache(10, time.Minute)

	if got := c.Len(); got != 0 {
		t.Errorf("empty Len = %d, want 0", got)
	}
	if got := c.Bytes(); got != 0 {
		t.Errorf("empty Bytes = %d, want 0", got)
	}

	c.Set("k1", []byte("hello"))
	c.Set("k2", []byte("xx"))

	if got := c.Len(); got != 2 {
		t.Errorf("Len = %d, want 2", got)
	}
	if got := c.Bytes(); got != 7 { // "hello"=5 + "xx"=2
		t.Errorf("Bytes = %d, want 7", got)
	}
}
