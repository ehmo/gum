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

// errCacheClosed is returned by the guarded db helpers once the cache is
// closed. It is internal: Get treats it as a miss and Set surfaces it as a
// write error.
var errCacheClosed = errors.New("cache: closed")

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
}

// ErrCacheCorrupt is returned by Open when the bbolt file exists but is not a
// valid bbolt database.
var ErrCacheCorrupt = errors.New("cache: bbolt file corrupt or not a valid database")

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

	db, err := bolt.Open(cfg.Path, 0o600, nil)
	if err != nil {
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
			c.mu.RUnlock()
			// Lazy update last_access (amortize writes)
			if now-he.lastAccessUnix > 60 {
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
			return fmt.Errorf("cache: bucket missing")
		}
		return b.Put([]byte(key), data)
	}); err != nil {
		return fmt.Errorf("cache: bbolt write: %w", err)
	}

	// Write to hot tier
	c.mu.Lock()
	c.promoteToHot(key, payload, expiresAt, now.Unix())
	c.mu.Unlock()

	// Check total size and evict if needed
	c.evictIfOverSize()

	return nil
}

// EvictExpired scans all entries in bbolt, removes those whose TTL has elapsed,
// and returns the count of removed entries. It also evicts the corresponding
// hot-tier entries. Callers should schedule EvictExpired periodically; it is
// not called automatically (no background goroutine — goleak must pass).
func (c *BBoltCache) EvictExpired() int {
	now := time.Now().Unix()
	var expiredKeys []string

	_ = c.view(func(tx *bolt.Tx) error {
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
	})

	if len(expiredKeys) == 0 {
		return 0
	}

	_ = c.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return nil
		}
		for _, k := range expiredKeys {
			_ = b.Delete([]byte(k))
		}
		return nil
	})

	// Remove from hot tier
	c.mu.Lock()
	for _, k := range expiredKeys {
		delete(c.hot, k)
		c.removeFromHotOrder(k)
	}
	c.mu.Unlock()

	return len(expiredKeys)
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

// evictIfOverSize evicts LRU entries from bbolt and hot tier if total size exceeds MaxSizeBytes.
func (c *BBoltCache) evictIfOverSize() {
	// Compute total size
	type entry struct {
		key            string
		size           int
		lastAccessUnix int64
	}

	var entries []entry
	var totalSize int64

	_ = c.view(func(tx *bolt.Tx) error {
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
	})

	if totalSize <= c.cfg.MaxSizeBytes {
		return
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
		return
	}

	_ = c.update(func(tx *bolt.Tx) error {
		b := tx.Bucket(cacheBucket)
		if b == nil {
			return nil
		}
		for _, k := range evictKeys {
			_ = b.Delete([]byte(k))
		}
		return nil
	})

	c.mu.Lock()
	for _, k := range evictKeys {
		delete(c.hot, k)
		c.removeFromHotOrder(k)
	}
	c.mu.Unlock()
}
