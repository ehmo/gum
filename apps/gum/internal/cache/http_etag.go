// Package cache — HTTP / ETag revalidation cache (spec §10.2).
//
// The §10.2 cache stores the validator upstream returned with a response
// alongside the response itself, keyed by
// `(op_id, variant_id_resolved, args_canonical, auth_subject_fingerprint)`.
// On the next identical call the kernel sends the validator as
// `If-None-Match`; a 304 means the stored body is still current and the
// caller gets `{"unchanged": true, "etag": "..."}` (§9.0).
//
// It is a different cache from the §10.3 semantic one. The semantic cache
// answers without an upstream request and expires on a TTL. This one always
// makes the request and has no TTL at all: an entry lives until `gum cache
// clear` removes it, because a validator does not go stale — the upstream
// decides that on every replay.

package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path"
	"sync/atomic"
	"time"
)

// HTTPStore is the byte-level backend an HTTPCache records entries in.
// *SQLiteWALCache satisfies it directly; BoltHTTPStore adapts *BBoltCache.
type HTTPStore interface {
	Get(key string) ([]byte, bool, error)
	Set(key string, value []byte, ttl time.Duration) error
}

// BoltHTTPStore adapts a *BBoltCache to HTTPStore. BBoltCache.Get reports an
// absent key and an unreadable record the same way, so the adapter never has
// a read error to pass on.
type BoltHTTPStore struct {
	C *BBoltCache
}

// Get returns the stored bytes for key.
func (b BoltHTTPStore) Get(key string) ([]byte, bool, error) {
	v, ok := b.C.Get(key)
	return v, ok, nil
}

// Set stores value under key.
func (b BoltHTTPStore) Set(key string, value []byte, ttl time.Duration) error {
	return b.C.Set(key, value, ttl)
}

// DeleteWhere removes the entries pred accepts.
func (b BoltHTTPStore) DeleteWhere(pred func(key string, payload []byte) bool) (int, error) {
	return b.C.DeleteWhere(pred)
}

// ForEachPayload walks the stored entries.
func (b BoltHTTPStore) ForEachPayload(fn func(key string, payload []byte)) error {
	return b.C.ForEachPayload(fn)
}

// HTTPPurger is the optional reclaim half of a store. §10.2 entries have no
// TTL, so `gum cache clear` is the only thing that removes them, and Get plus
// Set cannot express a delete. A store that does not implement it reports
// zero cleared rather than failing.
type HTTPPurger interface {
	DeleteWhere(pred func(key string, payload []byte) bool) (int, error)
}

// HTTPWalker is the optional reporting half of a store, for `gum cache stats`
// sizing a file another process wrote.
type HTTPWalker interface {
	ForEachPayload(fn func(key string, payload []byte)) error
}

// Both shipped backends are §10.2 stores. The assertion is here rather than in
// prose so the claim above cannot rot: BoltHTTPStore backs stdio mode and
// SQLiteWALCache is the WAL target `gum cache migrate` writes.
var (
	_ HTTPStore = BoltHTTPStore{}
	_ HTTPStore = (*SQLiteWALCache)(nil)
)

// HTTPKey derives the spec §10.2 four-component key. SHA-256 keeps the key
// bounded however large the canonical args blob is.
//
//   - opID: catalog op_id (resolved through aliases by step 1).
//   - variantID: resolved variant after §5.1.1 selection.
//   - argsCanonical: JCS-canonical args (caller normalizes).
//   - authFP: auth_subject_fingerprint (§10.0.1).
//
// The §10.3 key adds the field-mask projection as a fifth component. This one
// does not need it: the mask gum sends upstream is the `fields` query arg, so
// it is already inside argsCanonical, and two masks therefore key apart here
// as well.
func HTTPKey(opID, variantID, argsCanonical, authFP string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s", opID, variantID, argsCanonical, authFP)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// HTTPEntry is one stored `(etag, response)` pair.
type HTTPEntry struct {
	// ETag is the validator upstream returned. Never empty in a stored entry:
	// an entry without one could not produce a conditional request.
	ETag string `json:"etag"`
	// Body is the response the validator belongs to.
	Body []byte `json:"body"`
	// Format is the response format the adapter reported, carried so a 304
	// replay can serve the stored body in the shape it was stored in.
	Format string `json:"format"`
	// OpID names the operation the entry came from. The key is a digest, so
	// without this field nothing in the store is addressable by a human and
	// `gum cache clear <pattern>` (§9.0) has nothing to match.
	OpID string `json:"op_id,omitempty"`
}

// HTTPStats counts the lookups and stores this cache has served.
type HTTPStats struct {
	// Hits is the number of lookups that found a stored validator.
	Hits int64
	// Misses is the number of lookups that found none.
	Misses int64
	// Stores is the number of entries written.
	Stores int64
}

// HTTPCache is the spec §10.2 HTTP/ETag cache over an HTTPStore.
// Safe for concurrent use: the counters are atomic and the backends
// serialize their own writes.
type HTTPCache struct {
	store  HTTPStore
	hits   atomic.Int64
	misses atomic.Int64
	stores atomic.Int64
}

// NewHTTPCache wraps store. A nil store yields a nil cache, so a caller that
// could not open its backend can wire the result unconditionally: every
// method on a nil *HTTPCache is a no-op miss.
func NewHTTPCache(store HTTPStore) *HTTPCache {
	if store == nil {
		return nil
	}
	return &HTTPCache{store: store}
}

// Lookup returns the entry stored under key. A miss returns false, and so
// does a record the backend holds but this build cannot decode — a stored
// shape gum no longer understands must revalidate nothing rather than fail
// the call.
func (c *HTTPCache) Lookup(key string) (HTTPEntry, bool) {
	if c == nil {
		return HTTPEntry{}, false
	}
	raw, ok, err := c.store.Get(key)
	if err != nil || !ok {
		c.misses.Add(1)
		return HTTPEntry{}, false
	}
	var e HTTPEntry
	if err := json.Unmarshal(raw, &e); err != nil || e.ETag == "" {
		c.misses.Add(1)
		return HTTPEntry{}, false
	}
	c.hits.Add(1)
	return e, true
}

// Store writes e under key. An entry with no validator is dropped: storing it
// would cost disk and could never produce a conditional request. §10.2 gives
// the cache no TTL, so the entry is written without one.
func (c *HTTPCache) Store(key string, e HTTPEntry) error {
	if c == nil {
		return nil
	}
	if e.ETag == "" {
		return nil
	}
	payload, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("cache: marshal http entry: %w", err)
	}
	if err := c.store.Set(key, payload, 0); err != nil {
		return err
	}
	c.stores.Add(1)
	return nil
}

// ClearHTTP removes §10.2 entries whose op_id matches pattern and reports how
// many it deleted. An empty pattern clears the whole store.
//
// pattern is a path.Match glob matched against op_id, so `gmail.*` clears one
// API and `gmail.users.messages.list` clears one op. Entries stored before
// op_id was recorded, and rows whose payload no longer decodes, match only the
// empty pattern: a targeted clear must never delete something it cannot name.
func ClearHTTP(store HTTPStore, pattern string) (int, error) {
	purger, ok := store.(HTTPPurger)
	if !ok {
		return 0, nil
	}
	if pattern == "" {
		return purger.DeleteWhere(func(string, []byte) bool { return true })
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return 0, fmt.Errorf("cache: bad clear pattern %q: %w", pattern, err)
	}
	return purger.DeleteWhere(func(_ string, payload []byte) bool {
		var e HTTPEntry
		if err := json.Unmarshal(payload, &e); err != nil || e.OpID == "" {
			return false
		}
		matched, err := path.Match(pattern, e.OpID)
		return err == nil && matched
	})
}

// HTTPUsage is what one §10.2 store holds, for `gum cache stats`.
type HTTPUsage struct {
	// Entries is the number of stored validators.
	Entries int64
	// Bytes is the total size of the stored payloads.
	Bytes int64
}

// MeasureHTTP walks store and sizes it. A store that cannot walk reports an
// empty usage, which is what an unmeasurable backend truthfully knows.
func MeasureHTTP(store HTTPStore) (HTTPUsage, error) {
	walker, ok := store.(HTTPWalker)
	if !ok {
		return HTTPUsage{}, nil
	}
	var u HTTPUsage
	err := walker.ForEachPayload(func(_ string, payload []byte) {
		u.Entries++
		u.Bytes += int64(len(payload))
	})
	return u, err
}

// Stats returns a snapshot of the counters.
func (c *HTTPCache) Stats() HTTPStats {
	if c == nil {
		return HTTPStats{}
	}
	return HTTPStats{
		Hits:   c.hits.Load(),
		Misses: c.misses.Load(),
		Stores: c.stores.Load(),
	}
}
