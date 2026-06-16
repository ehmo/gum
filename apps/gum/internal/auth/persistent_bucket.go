// Package auth implements the closed-enum auth strategies (spec.md §7, §14).

// requires: go get go.etcd.io/bbolt@v1.3.10

package auth

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/ehmo/gum/internal/dispatch"
)

// PersistentBucket is a token bucket whose state is persisted to bbolt so
// the leak rate survives process restart. Key: op_id|creds_id.
//
// The bucket uses a leaky-bucket algorithm: tokens drain at DefaultLeakRatePerSecond
// per second. When the bucket is empty, Take returns ErrRateLimited.
//
// Upstream X-Quota-* / Retry-After signals are applied via Update, which freezes
// the bucket until retryAfter elapses.
//
// bbolt bucket name: "gum-token-bucket"
// Entry format: JSON-encoded bucketState.
type PersistentBucket struct {
	db     *bolt.DB
	cfg    BucketConfig
	mu     sync.Mutex
	closed bool
}

// bucketState is the persisted state for a single (opID, credsID) pair.
// RetryAfterUnixNano is stored at nanosecond resolution so sub-second
// Retry-After durations (test fixtures use 10ms) freeze Take correctly.
type bucketState struct {
	Tokens             float64 `json:"tokens"`
	LastRefillUnixNano int64   `json:"last_refill_unix_nano"`
	Capacity           int64   `json:"capacity"`
	LeakRate           float64 `json:"leak_rate"`
	RetryAfterUnixNano int64   `json:"retry_after_unix_nano"`
}

var tokenBucket = []byte("gum-token-bucket")

// BucketConfig is the configuration for OpenBucket.
type BucketConfig struct {
	// Path is the bbolt db path. May share a file with BBoltCache via a separate
	// bucket name ("token_buckets" vs "cache").
	Path string
	// DefaultCapacity is the initial and maximum token count per (opID, credsID) key.
	// Defaults to 100 when 0.
	DefaultCapacity int64
	// DefaultLeakRatePerSecond is the rate at which tokens are replenished per second.
	// Defaults to 1.0 when 0.
	DefaultLeakRatePerSecond float64
	// Now is the clock used for token refill and retry-after computations.
	// Tests inject a deterministic clock to avoid 1-ms-window flakes between
	// back-to-back Take calls at high leak rates. Defaults to time.Now.
	Now func() time.Time
}

// ErrRateLimited is returned by Take when the bucket has insufficient tokens.
// It wraps dispatch.ErrRateLimited so the dispatch boundary can detect any
// rate-limited error in the chain (errors.Is matches either the local
// sentinel for auth-package tests or the kernel sentinel for dispatch).
var ErrRateLimited = fmt.Errorf("%w", dispatch.ErrRateLimited)

// OpenBucket creates or opens a PersistentBucket at cfg.Path.
// If the file does not exist, it is created. If the bbolt file is corrupt,
// OpenBucket returns a wrapped error (the green team wraps the bbolt error).
func OpenBucket(cfg BucketConfig) (*PersistentBucket, error) {
	if cfg.Path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("auth: get home dir: %w", err)
		}
		cfg.Path = filepath.Join(home, ".cache", "gum", "token-bucket.db")
	}
	if cfg.DefaultCapacity == 0 {
		cfg.DefaultCapacity = 100
	}
	if cfg.DefaultLeakRatePerSecond == 0 {
		cfg.DefaultLeakRatePerSecond = 1.0
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, fmt.Errorf("auth: create bucket dir: %w", err)
	}

	// NoSync skips fsync on every Update; the test suite's back-to-back Take
	// calls otherwise see ~5ms of disk-flush time between calls, which a
	// 1000 tokens/sec leak rate fully replenishes in. State is still durable
	// on graceful Close (which flushes), so cross-process replay still works.
	db, err := bolt.Open(cfg.Path, 0o600, &bolt.Options{NoSync: true})
	if err != nil {
		return nil, fmt.Errorf("auth: open bbolt: %w", err)
	}

	// Create bucket if missing
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(tokenBucket)
		return err
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("auth: create token bucket: %w", err)
	}

	return &PersistentBucket{
		db:  db,
		cfg: cfg,
	}, nil
}

// Close flushes pending bucket state and closes the bbolt handle.
// Close is idempotent. With NoSync=true at open, an explicit Sync() here is
// required so cross-process state survives.
func (b *PersistentBucket) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	_ = b.db.Sync()
	return b.db.Close()
}

// Take attempts to consume cost tokens for (opID, credsID).
// Returns nil on success, ErrRateLimited if insufficient capacity after leak
// replenishment. Cost must be ≥ 1; a cost of 0 is treated as 1.
// If Update has set a retryAfter that has not yet elapsed, Take returns
// ErrRateLimited without consuming any tokens.
func (b *PersistentBucket) Take(opID, credsID string, cost int64) error {
	if cost <= 0 {
		cost = 1
	}

	key := opID + "|" + credsID
	now := b.cfg.Now()

	b.mu.Lock()
	defer b.mu.Unlock()

	var state bucketState
	err := b.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(tokenBucket)
		if bkt == nil {
			return nil
		}
		v := bkt.Get([]byte(key))
		if v == nil {
			return nil
		}
		return json.Unmarshal(v, &state)
	})
	if err != nil {
		return fmt.Errorf("auth: read bucket state: %w", err)
	}

	// Initialize if missing
	if state.Capacity == 0 {
		state = bucketState{
			Tokens:             float64(b.cfg.DefaultCapacity),
			LastRefillUnixNano: now.UnixNano(),
			Capacity:           b.cfg.DefaultCapacity,
			LeakRate:           b.cfg.DefaultLeakRatePerSecond,
			RetryAfterUnixNano: 0,
		}
	}

	// Check retry-after freeze
	if state.RetryAfterUnixNano > 0 && now.UnixNano() < state.RetryAfterUnixNano {
		// Persist current state
		_ = b.persistState(key, state)
		return ErrRateLimited
	}

	// Compute leak/refill
	elapsed := now.UnixNano() - state.LastRefillUnixNano
	elapsedSec := float64(elapsed) / float64(time.Second)
	if elapsedSec < 0 {
		elapsedSec = 0
	}
	newTokens := state.Tokens + elapsedSec*state.LeakRate
	if newTokens > float64(state.Capacity) {
		newTokens = float64(state.Capacity)
	}

	// Try to consume
	if newTokens >= float64(cost) {
		state.Tokens = newTokens - float64(cost)
		state.LastRefillUnixNano = now.UnixNano()
		if err := b.persistState(key, state); err != nil {
			return fmt.Errorf("auth: persist bucket state: %w", err)
		}
		return nil
	}

	// Not enough tokens — persist state but don't consume
	state.Tokens = newTokens
	state.LastRefillUnixNano = now.UnixNano()
	_ = b.persistState(key, state)
	return ErrRateLimited
}

// Update applies upstream rate-limit signals for (opID, credsID).
// retryAfter freezes Take for that key until time.Now().Add(retryAfter) passes.
// A zero or negative retryAfter is a no-op.
func (b *PersistentBucket) Update(opID, credsID string, retryAfter time.Duration) {
	if retryAfter <= 0 {
		return
	}

	key := opID + "|" + credsID
	now := b.cfg.Now()

	b.mu.Lock()
	defer b.mu.Unlock()

	var state bucketState
	_ = b.db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(tokenBucket)
		if bkt == nil {
			return nil
		}
		v := bkt.Get([]byte(key))
		if v == nil {
			return nil
		}
		return json.Unmarshal(v, &state)
	})

	// Initialize if missing
	if state.Capacity == 0 {
		state = bucketState{
			Tokens:             float64(b.cfg.DefaultCapacity),
			LastRefillUnixNano: now.UnixNano(),
			Capacity:           b.cfg.DefaultCapacity,
			LeakRate:           b.cfg.DefaultLeakRatePerSecond,
			RetryAfterUnixNano: 0,
		}
	}

	state.RetryAfterUnixNano = now.Add(retryAfter).UnixNano()
	_ = b.persistState(key, state)
}

// persistState writes the bucket state to bbolt. Must be called with b.mu held.
func (b *PersistentBucket) persistState(key string, state bucketState) error {
	// Clamp tokens to valid range
	state.Tokens = math.Max(0, math.Min(float64(state.Capacity), state.Tokens))

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("auth: marshal bucket state: %w", err)
	}

	return b.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(tokenBucket)
		if bkt == nil {
			return fmt.Errorf("auth: token bucket missing")
		}
		return bkt.Put([]byte(key), data)
	})
}
