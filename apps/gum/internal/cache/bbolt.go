// Package cache holds the per-credential response cache (spec.md §3.1 step 4, §14).

// requires: go get go.etcd.io/bbolt@v1.3.10

package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// hotLastAccessRefreshSeconds is how stale a hot entry's last-access stamp may
// get before a Get rewrites it. Refreshing on every hit would take the write
// lock on every read.
const hotLastAccessRefreshSeconds = 60

// errCacheClosed is returned by the guarded db helpers once the cache is
// closed. It is internal: Get treats it as a miss and Set surfaces it as a
// write error.
var errCacheClosed = errors.New("cache: closed")

// errBucketMissing means the cache file exists but lacks the bucket Open
// creates, so the database is not a gum cache.
var errBucketMissing = errors.New("cache: bucket missing")

// BBoltCache is a process-restart-surviving cache backed by bbolt at the
// configured path (default ~/.cache/gum/cache.db). Hot tier is the existing
// in-memory map; bbolt is the source of truth.
//
// Bucket layout:
//
//	"gum-cache" bucket: key → JSON-encoded cacheRecord{payload, expires_at_unix, size, last_access_unix}
//
// The hot tier is a map[string]*hotEntry with HotTierSize entries; on every Get/Set the
// hot tier is consulted first before touching bbolt.
type BBoltCache struct {
	db       *bolt.DB
	cfg      BBoltConfig
	mu       sync.RWMutex
	hot      map[string]*hotEntry
	hotOrder []string // LRU order, oldest first
	closed   bool
}

// hotEntry is an entry in the in-memory hot tier.
type hotEntry struct {
	payload        []byte
	expiresAtUnix  int64 // 0 = never expires
	lastAccessUnix int64
}

// cacheRecord is the on-disk JSON format for a cache entry.
type cacheRecord struct {
	Payload        []byte `json:"payload"`
	ExpiresAtUnix  int64  `json:"expires_at_unix"`
	Size           int    `json:"size"`
	LastAccessUnix int64  `json:"last_access_unix"`
}

var cacheBucket = []byte("gum-cache")

// BBoltConfig is the configuration for Open.
type BBoltConfig struct {
	// Path is the filesystem path to the bbolt database file.
	// Defaults to ~/.cache/gum/cache.db when empty.
	Path string
	// MaxSizeBytes is the maximum total byte size of stored payloads.
	// Defaults to 256 MiB (256 << 20) when 0.
	MaxSizeBytes int64
	// HotTierSize is the maximum number of entries kept in the in-memory hot tier.
	// Defaults to 512 when 0.
	HotTierSize int
	// OpenTimeout bounds the wait for the file lock another process holds.
	// Zero waits indefinitely, which is bbolt's own default. A caller that
	// opens the cache on a latency path passes a short value and treats
	// ErrCacheLocked as "run without this cache".
	OpenTimeout time.Duration
}

// ErrCacheCorrupt is returned by Open when the bbolt file exists but is not a
// valid bbolt database.
var ErrCacheCorrupt = errors.New("cache: bbolt file corrupt or not a valid database")

// ErrCacheLocked is returned by Open when another process still holds the
// file lock after BBoltConfig.OpenTimeout. The file itself is intact, so the
// caller either retries later or runs without the cache.
var ErrCacheLocked = errors.New("cache: bbolt file locked by another process")

// Open creates or opens a BBoltCache at cfg.Path.
// If cfg.Path does not exist, Open creates it along with any missing parent directories.
// Returns ErrCacheCorrupt if the file exists but cannot be opened as a bbolt database.
func Open(cfg BBoltConfig) (*BBoltCache, error) {
	if cfg.Path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("cache: get home dir: %w", err)
		}
		cfg.Path = filepath.Join(home, ".cache", "gum", "cache.db")
	}
	if cfg.MaxSizeBytes == 0 {
		cfg.MaxSizeBytes = 256 << 20
	}
	if cfg.HotTierSize == 0 {
		cfg.HotTierSize = 256
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o700); err != nil {
		return nil, fmt.Errorf("cache: create cache dir: %w", err)
	}

	var opts *bolt.Options
	if cfg.OpenTimeout > 0 {
		opts = &bolt.Options{Timeout: cfg.OpenTimeout}
	}
	db, err := bolt.Open(cfg.Path, 0o600, opts)
	if err != nil {
		// A held lock is not corruption: the recovery is to wait or to skip
		// the cache, not to delete the file, so it gets its own sentinel.
		if errors.Is(err, bolt.ErrTimeout) {
			return nil, fmt.Errorf("%w: %s", ErrCacheLocked, cfg.Path)
		}
		return nil, fmt.Errorf("%w: %v", ErrCacheCorrupt, err)
	}

	// Create bucket if missing
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(cacheBucket)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("cache: create bucket: %w", err)
	}

	return &BBoltCache{
		db:       db,
		cfg:      cfg,
		hot:      make(map[string]*hotEntry),
		hotOrder: make([]string, 0),
	}, nil
}

// Close flushes the hot tier to bbolt, syncs, and closes the file handle.
// Close is idempotent; calling it twice returns nil on the second call.
func (c *BBoltCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.db.Close()
}

// view runs fn in a bbolt read transaction while holding the read lock. Because
// Close takes the write lock, an in-flight view blocks Close from closing the
// handle mid-transaction, and a view started after Close sees closed and bails
// — closing the use-after-close panic window (review gum-8aqm). It must NOT be
// called while already holding c.mu (the RLock would deadlock against a writer).
func (c *BBoltCache) view(fn func(*bolt.Tx) error) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return errCacheClosed
	}
	return c.db.View(fn)
}

// update is the read/write counterpart of view; same closed-safety contract.
func (c *BBoltCache) update(fn func(*bolt.Tx) error) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return errCacheClosed
	}
	return c.db.Update(fn)
}

// Get returns the cached payload and true if present and unexpired.
// Key shape (from KeyFor): op_id|args_canonical_sha256|scope_hash|creds_id.
// A miss (absent or TTL elapsed) returns (nil, false).
func (c *BBoltCache) Get(key string) ([]byte, bool) {
	now := time.Now().Unix()

	// Check hot tier first
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return nil, false
	}
	if he, ok := c.hot[key]; ok {
		if he.expiresAtUnix == 0 || he.expiresAtUnix > now {
			payload := make([]byte, len(he.payload))
			copy(payload, he.payload)
			// Copy lastAccessUnix while the read lock still holds it. The write
			// below mutates that same field of that same pointer under the
			// write lock, so reading it afterwards is a data race.
			lastAccess := he.lastAccessUnix
			c.mu.RUnlock()
			// Lazy update last_access (amortize writes)
			if now-lastAccess > hotLastAccessRefreshSeconds {
				c.mu.Lock()
				if he2, ok2 := c.hot[key]; ok2 {
					he2.lastAccessUnix = now
				}
				c.mu.Unlock()
			}
			return payload, true
		}
		// Expired in hot tier
		c.mu.RUnlock()
		c.mu.Lock()
		delete(c.hot, key)
		c.removeFromHotOrder(key)
		c.mu.Unlock()
		return nil, false
	}
	c.mu.RUnlock()

	// Check bbolt
	var record cacheRecord
	found := false
	_ = c.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return nil
		}
		v := b.Get([]byte(key))
		if v == nil {
			return nil
		}
		if err := json.Unmarshal(v, &record); err != nil {
			return nil
		}
		found = true
		return nil
	})

	if !found {
		return nil, false
	}

	// Check expiry
	if record.ExpiresAtUnix != 0 && record.ExpiresAtUnix <= now {
		return nil, false
	}

	// Promote to hot tier
	c.mu.Lock()
	c.promoteToHot(key, record.Payload, record.ExpiresAtUnix, now)
	c.mu.Unlock()

	payload := make([]byte, len(record.Payload))
	copy(payload, record.Payload)
	return payload, true
}

// Set stores payload under key with the given TTL.
// When TTL is 0, the entry never expires.
// Returns an error only on bbolt I/O failure; in-memory hot-tier failures are
// non-fatal (the bbolt write is the authoritative path).
func (c *BBoltCache) Set(key string, payload []byte, ttl time.Duration) error {
	now := time.Now()
	var expiresAt int64
	if ttl > 0 {
		expiresAt = now.Add(ttl).Unix()
	}

	record := cacheRecord{
		Payload:        payload,
		ExpiresAtUnix:  expiresAt,
		Size:           len(payload),
		LastAccessUnix: now.Unix(),
	}

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("cache: marshal record: %w", err)
	}

	if err := c.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return errBucketMissing
		}
		return b.Put([]byte(key), data)
	}); err != nil {
		return fmt.Errorf("cache: bbolt write: %w", err)
	}

	// Write to hot tier
	c.mu.Lock()
	c.promoteToHot(key, payload, expiresAt, now.Unix())
	c.mu.Unlock()

	// Check total size and evict if needed. A failed eviction leaves the cache
	// over MaxSizeBytes, so the caller hears about it instead of the cache
	// growing without bound in silence.
	if err := c.evictIfOverSize(); err != nil {
		return err
	}

	return nil
}

// EvictExpired scans all entries in bbolt, removes those whose TTL has elapsed,
// and returns the count of entries it actually deleted. It also evicts the
// corresponding hot-tier entries. Callers should schedule EvictExpired
// periodically; it is not called automatically (no background goroutine —
// goleak must pass).
//
// The count is the number of committed deletions, not the number of expired
// keys the scan found. When the delete transaction fails the count is 0 and the
// error says why, so `gum cache clear --expired` cannot report entries as
// removed that are still on disk (review gum-fbst).
func (c *BBoltCache) EvictExpired() (int, error) {
	now := time.Now().Unix()
	var expiredKeys []string

	if err := c.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var record cacheRecord
			if err := json.Unmarshal(v, &record); err != nil {
				expiredKeys = append(expiredKeys, string(k))
				return nil
			}
			if record.ExpiresAtUnix != 0 && record.ExpiresAtUnix <= now {
				expiredKeys = append(expiredKeys, string(k))
			}
			return nil
		})
	}); err != nil {
		return 0, fmt.Errorf("cache: scan for expired entries: %w", err)
	}

	if len(expiredKeys) == 0 {
		return 0, nil
	}

	deleted := 0
	if err := c.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return errBucketMissing
		}
		for _, k := range expiredKeys {
			if err := b.Delete([]byte(k)); err != nil {
				return fmt.Errorf("delete %q: %w", k, err)
			}
			deleted++
		}
		return nil
	}); err != nil {
		// The transaction rolled back, so nothing was deleted and the hot tier
		// must keep mirroring what is still on disk.
		return 0, fmt.Errorf("cache: evict expired entries: %w", err)
	}

	// Remove from hot tier
	c.mu.Lock()
	for _, k := range expiredKeys {
		delete(c.hot, k)
		c.removeFromHotOrder(k)
	}
	c.mu.Unlock()

	return deleted, nil
}

// DeleteWhere removes every entry whose stored payload pred accepts and
// returns how many it deleted. It is the reclaim path for caches with no TTL:
// §10.2 entries never expire, so the only way to drop them is to name them.
//
// pred sees the decoded payload, not the on-disk record, so a caller matches
// on its own entry format without knowing this file's. An entry whose record
// does not decode is passed to pred as a nil payload; a pred that accepts nil
// therefore also reaps corrupt rows.
func (c *BBoltCache) DeleteWhere(pred func(key string, payload []byte) bool) (int, error) {
	if pred == nil {
		return 0, nil
	}
	var doomed []string

	if err := c.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var record cacheRecord
			if err := json.Unmarshal(v, &record); err != nil {
				if pred(string(k), nil) {
					doomed = append(doomed, string(k))
				}
				return nil
			}
			if pred(string(k), record.Payload) {
				doomed = append(doomed, string(k))
			}
			return nil
		})
	}); err != nil {
		return 0, fmt.Errorf("cache: scan for matching entries: %w", err)
	}

	if len(doomed) == 0 {
		return 0, nil
	}

	deleted := 0
	if err := c.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return errBucketMissing
		}
		for _, k := range doomed {
			if err := b.Delete([]byte(k)); err != nil {
				return fmt.Errorf("delete %q: %w", k, err)
			}
			deleted++
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("cache: delete matching entries: %w", err)
	}

	c.mu.Lock()
	for _, k := range doomed {
		delete(c.hot, k)
		c.removeFromHotOrder(k)
	}
	c.mu.Unlock()

	return deleted, nil
}

// ForEachPayload calls fn for every stored entry that decodes. It exists so a
// reporting caller (`gum cache stats`) can size a store it did not write,
// without exposing the on-disk record layout.
func (c *BBoltCache) ForEachPayload(fn func(key string, payload []byte)) error {
	if fn == nil {
		return nil
	}
	return c.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var record cacheRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return nil
			}
			fn(string(k), record.Payload)
			return nil
		})
	})
}

// promoteToHot adds/updates an entry in the hot tier. Must be called with c.mu held (write).
func (c *BBoltCache) promoteToHot(key string, payload []byte, expiresAt, lastAccess int64) {
	cp := make([]byte, len(payload))
	copy(cp, payload)

	if _, exists := c.hot[key]; !exists {
		c.hotOrder = append(c.hotOrder, key)
	}
	c.hot[key] = &hotEntry{
		payload:        cp,
		expiresAtUnix:  expiresAt,
		lastAccessUnix: lastAccess,
	}

	// Evict oldest hot entries if over HotTierSize
	for len(c.hot) > c.cfg.HotTierSize {
		if len(c.hotOrder) == 0 {
			break
		}
		oldest := c.hotOrder[0]
		c.hotOrder = c.hotOrder[1:]
		delete(c.hot, oldest)
	}
}

// removeFromHotOrder removes a key from the hotOrder slice. Must be called with c.mu held (write).
func (c *BBoltCache) removeFromHotOrder(key string) {
	for i, k := range c.hotOrder {
		if k == key {
			c.hotOrder = append(c.hotOrder[:i], c.hotOrder[i+1:]...)
			return
		}
	}
}

// evictIfOverSize evicts LRU entries from bbolt and hot tier if total size
// exceeds MaxSizeBytes. It returns the first error that stopped the eviction;
// the caller must not treat a failed sweep as a cache that stayed under the cap.
func (c *BBoltCache) evictIfOverSize() error {
	// Compute total size
	type entry struct {
		key            string
		size           int
		lastAccessUnix int64
	}

	var entries []entry
	var totalSize int64

	if err := c.view(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var record cacheRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return nil
			}
			totalSize += int64(record.Size)
			entries = append(entries, entry{
				key:            string(k),
				size:           record.Size,
				lastAccessUnix: record.LastAccessUnix,
			})
			return nil
		})
	}); err != nil {
		return fmt.Errorf("cache: scan for over-size eviction: %w", err)
	}

	if totalSize <= c.cfg.MaxSizeBytes {
		return nil
	}

	// Sort by lastAccessUnix ascending (oldest first). sort.Slice is O(n log n);
	// the previous hand-rolled double loop was O(n²) and ran on every Set once
	// the cache filled (review gum-yvam).
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastAccessUnix < entries[j].lastAccessUnix
	})

	// Evict oldest until under cap
	var evictKeys []string
	for _, e := range entries {
		if totalSize <= c.cfg.MaxSizeBytes {
			break
		}
		evictKeys = append(evictKeys, e.key)
		totalSize -= int64(e.size)
	}

	if len(evictKeys) == 0 {
		return nil
	}

	if err := c.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return errBucketMissing
		}
		for _, k := range evictKeys {
			if err := b.Delete([]byte(k)); err != nil {
				return fmt.Errorf("delete %q: %w", k, err)
			}
		}
		return nil
	}); err != nil {
		// Rolled back: the entries are still on disk, so the hot tier keeps them.
		return fmt.Errorf("cache: evict over-size entries: %w", err)
	}

	c.mu.Lock()
	for _, k := range evictKeys {
		delete(c.hot, k)
		c.removeFromHotOrder(k)
	}
	c.mu.Unlock()

	return nil
}
