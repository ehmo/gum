// Package cache holds the per-credential response cache (spec.md §3.1 step 4, §14).
package cache

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// Stats holds cache performance counters.
type Stats struct {
	// Hits is the number of successful cache lookups.
	Hits int64
	// Misses is the number of failed cache lookups (key not found or expired).
	Misses int64
	// Evictions is the number of entries evicted due to maxEntries overflow.
	Evictions int64
}

// entry is a single cache record stored inside the LRU list.
type entry struct {
	key    string
	value  []byte
	expiry time.Time // zero means never expires
}

// MemCache is an in-process LRU cache with per-entry TTL (spec.md §3.1 step 4).
// It is safe for concurrent use. maxEntries=0 means unlimited. Eviction is LRU;
// TTL is enforced lazily at Get time (no background goroutine; goleak-safe).
type MemCache struct {
	mu         sync.Mutex
	maxEntries int
	ttl        time.Duration
	ll         *list.List
	items      map[string]*list.Element
	stats      Stats
}

// NewMemCache constructs a MemCache.
//   - maxEntries: maximum number of live entries (LRU eviction when exceeded).
//   - ttl: time after Set that an entry expires. 0 = never expires.
func NewMemCache(maxEntries int, ttl time.Duration) *MemCache {
	return &MemCache{
		maxEntries: maxEntries,
		ttl:        ttl,
		ll:         list.New(),
		items:      make(map[string]*list.Element),
	}
}

// Get returns the cached value for key. ok=false means a miss (absent or expired).
func (c *MemCache) Get(key string) (value []byte, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, found := c.items[key]
	if !found {
		c.stats.Misses++
		return nil, false
	}

	e := el.Value.(*entry)

	// Check TTL expiry (zero expiry = never expires).
	if !e.expiry.IsZero() && time.Now().After(e.expiry) {
		c.ll.Remove(el)
		delete(c.items, key)
		c.stats.Misses++
		return nil, false
	}

	// Move to front (most recently used).
	c.ll.MoveToFront(el)
	c.stats.Hits++
	// Return a copy to avoid mutation of the stored slice.
	result := make([]byte, len(e.value))
	copy(result, e.value)
	return result, true
}

// Set stores value under key, evicting the LRU entry if maxEntries is exceeded.
func (c *MemCache) Set(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var expiry time.Time
	if c.ttl > 0 {
		expiry = time.Now().Add(c.ttl)
	}

	// Update existing entry.
	if el, found := c.items[key]; found {
		e := el.Value.(*entry)
		// Store a copy.
		cp := make([]byte, len(value))
		copy(cp, value)
		e.value = cp
		e.expiry = expiry
		c.ll.MoveToFront(el)
		return
	}

	// Store a copy.
	cp := make([]byte, len(value))
	copy(cp, value)

	e := &entry{key: key, value: cp, expiry: expiry}
	el := c.ll.PushFront(e)
	c.items[key] = el

	// Evict LRU entry if over capacity.
	if c.maxEntries > 0 && c.ll.Len() > c.maxEntries {
		tail := c.ll.Back()
		if tail != nil {
			tailEntry := tail.Value.(*entry)
			c.ll.Remove(tail)
			delete(c.items, tailEntry.key)
			c.stats.Evictions++
		}
	}
}

// Stats returns a snapshot of the cache performance counters.
func (c *MemCache) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// Len returns the number of live entries currently in the cache.
func (c *MemCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Bytes returns the sum of len(value) for all live entries.
// The cache is small in v0.1.0 so a full iteration under the mutex is acceptable.
func (c *MemCache) Bytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var total int64
	for el := c.ll.Front(); el != nil; el = el.Next() {
		e := el.Value.(*entry)
		total += int64(len(e.value))
	}
	return total
}

// KeyFor derives a deterministic cache key from the four components.
// Uses SHA-256 so long argument canonicalisations don't bloat the key.
//
//   - opID: the catalog op_id (e.g. "gmail.users.messages.list")
//   - argsCanonical: stable serialisation of the invocation args (caller normalises)
//   - scopeHash: hash of the OAuth scopes used for the request
//   - credsID: opaque credential identity (e.g. sub claim or client_id)
func KeyFor(opID, argsCanonical, scopeHash, credsID string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s", opID, argsCanonical, scopeHash, credsID)
	return fmt.Sprintf("%x", h.Sum(nil))
}
