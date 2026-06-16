// Package cache — semantic response cache (spec §10.3).
//
// Keys reference (op_id, variant_id, args_canonical, fields,
// auth_subject_fingerprint) — five components. The HTTP/ETag cache in
// §10.2 keys on four components; the §10.3 semantic cache adds `fields`
// (the active field-mask projection) so two callers requesting different
// projections of the same upstream payload don't collide.
//
// VAAC eviction (spec §10.3 line 2255): when the entry count exceeds
// MaxEntries, the cache scores live entries by
//
//	(frequency × value) / (recency × freshness_decay)
//
// and evicts the lowest-scoring tail. Frequency is the access count since
// insert; value is the stored byte size (a stable, principal-equal proxy
// for the savings a hit avoids); recency is wall-clock seconds since the
// last Get; freshness_decay grows as the entry approaches its per-op TTL,
// biasing eviction toward soon-stale entries.

package cache

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"time"
)

// DefaultSemanticTTL is the fallback TTL applied to ops that don't have an
// explicit per-op entry in PerOpTTL. Spec §10.3 lists targeted defaults for
// calendar/drive/gmail; everything else inherits this short window so an
// unconfigured op doesn't accidentally pin stale data for hours.
const DefaultSemanticTTL = 60 * time.Second

// PerOpTTL is the spec §10.3 line 2254 TTL table for the semantic cache.
// Operators MAY override via a future `gum config set cache.ttl.<op_id>=...`
// hook (deferred); v0.1.0 ships the spec-listed defaults verbatim.
var PerOpTTL = map[string]time.Duration{
	// calendar.events: 60s
	"google.calendar.calendars.events.list": 60 * time.Second,
	"google.calendar.calendars.events.get":  60 * time.Second,
	"calendar.events.list":                  60 * time.Second,
	"calendar.events.get":                   60 * time.Second,
	// drive.files.list: 300s
	"google.drive.files.list": 300 * time.Second,
	"drive.files.list":        300 * time.Second,
	// gmail.profiles.get: 3600s
	"google.gmail.users.getProfile": 3600 * time.Second,
	"gmail.profiles.get":            3600 * time.Second,
	// User-immutable references: 24h. Conservative — only ops where the
	// upstream payload is genuinely immutable within a 24h window go here.
	"google.youtube.i18nLanguages.list": 24 * time.Hour,
	"google.youtube.i18nRegions.list":   24 * time.Hour,
}

// SemanticKey derives the spec §10.3 5-component cache key. SHA-256 keeps
// the key bounded even when the canonicalized args blob is large.
//
//   - opID: catalog op_id (resolved through aliases by step 1).
//   - variantID: resolved variant after §5.1.1 selection.
//   - argsCanonical: JCS-canonical args (caller normalizes).
//   - fields: the active field-mask projection ("" when no profile).
//   - authFP: auth_subject_fingerprint (§10.0.1), "" in unauthenticated stubs.
func SemanticKey(opID, variantID, argsCanonical, fields, authFP string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s", opID, variantID, argsCanonical, fields, authFP)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// SemanticCache is the spec §10.3 in-process semantic response cache.
// Wraps an LRU MemCache for storage and layers per-op TTL + VAAC scoring on
// top. Persistent on-disk storage (semantic.db) is reserved for v0.2.0;
// v0.1.0 ships the in-process layer so `gum.cache_stats` can surface
// non-zero semantic.hits per the bead acceptance.
type SemanticCache struct {
	mu                 sync.Mutex
	maxEntries         int
	defaultTTL         time.Duration
	perOpTTL           map[string]time.Duration
	entries            map[string]*semanticEntry
	stats              Stats
	freshnessTTLFactor float64 // exponent applied to freshness_decay; default 1.0
}

// semanticEntry is one row in the semantic cache. Tracks both the payload
// (with expiry) and the VAAC scoring inputs (frequency, value, recency).
type semanticEntry struct {
	value      []byte
	opID       string
	expiry     time.Time // zero = never expires (spec §10.3 has no never; reserved)
	insertedAt time.Time
	lastAccess time.Time
	hitCount   int64
}

// SemanticConfig configures NewSemanticCache. MaxEntries=0 means unbounded
// (tests/embedders). DefaultTTL=0 inherits DefaultSemanticTTL.
type SemanticConfig struct {
	MaxEntries int
	DefaultTTL time.Duration
	// PerOpTTL overrides PerOpTTL when non-nil. Nil falls back to the
	// package-level spec table.
	PerOpTTL map[string]time.Duration
}

// NewSemanticCache builds a SemanticCache from cfg. The returned instance
// is safe for concurrent use.
func NewSemanticCache(cfg SemanticConfig) *SemanticCache {
	ttl := cfg.DefaultTTL
	if ttl == 0 {
		ttl = DefaultSemanticTTL
	}
	perOp := cfg.PerOpTTL
	if perOp == nil {
		perOp = PerOpTTL
	}
	return &SemanticCache{
		maxEntries:         cfg.MaxEntries,
		defaultTTL:         ttl,
		perOpTTL:           perOp,
		entries:            make(map[string]*semanticEntry),
		freshnessTTLFactor: 1.0,
	}
}

// TTLForOp returns the per-op TTL or the configured default when none is
// registered. Exported so the dispatcher (and tests) can introspect the
// effective lifetime without re-deriving it.
func (s *SemanticCache) TTLForOp(opID string) time.Duration {
	if ttl, ok := s.perOpTTL[opID]; ok {
		return ttl
	}
	return s.defaultTTL
}

// Get returns the cached payload and true on a hit. Misses (absent or TTL
// elapsed) return (nil, false). Hits increment the entry's frequency and
// update last-access for VAAC scoring.
func (s *SemanticCache) Get(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.entries[key]
	if !ok {
		s.stats.Misses++
		return nil, false
	}
	now := time.Now()
	if !rec.expiry.IsZero() && now.After(rec.expiry) {
		delete(s.entries, key)
		s.stats.Misses++
		return nil, false
	}
	rec.hitCount++
	rec.lastAccess = now
	s.stats.Hits++
	out := make([]byte, len(rec.value))
	copy(out, rec.value)
	return out, true
}

// Set stores value under key with the TTL registered for opID (falling
// back to the configured default). If maxEntries is exceeded, the VAAC
// scorer picks the lowest-scoring entry for eviction.
func (s *SemanticCache) Set(key string, value []byte, opID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ttl := s.defaultTTL
	if t, ok := s.perOpTTL[opID]; ok {
		ttl = t
	}
	now := time.Now()
	var expiry time.Time
	if ttl > 0 {
		expiry = now.Add(ttl)
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	if existing, ok := s.entries[key]; ok {
		existing.value = cp
		existing.opID = opID
		existing.expiry = expiry
		// Preserve hit count + insert time so warm entries don't lose
		// their VAAC standing when refreshed by a re-fetch.
		existing.lastAccess = now
		return
	}
	s.entries[key] = &semanticEntry{
		value:      cp,
		opID:       opID,
		expiry:     expiry,
		insertedAt: now,
		lastAccess: now,
		hitCount:   0,
	}
	s.evictIfOverCapacity(now)
}

// Stats returns a snapshot of the hit/miss/eviction counters.
func (s *SemanticCache) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

// Len returns the number of live entries.
func (s *SemanticCache) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Bytes returns the sum of stored value lengths. Linear scan acceptable
// at v0.1 cache sizes; revisit when persistent storage lands.
func (s *SemanticCache) Bytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	for _, rec := range s.entries {
		total += int64(len(rec.value))
	}
	return total
}

// evictIfOverCapacity is the VAAC scorer. Caller must hold s.mu.
func (s *SemanticCache) evictIfOverCapacity(now time.Time) {
	if s.maxEntries <= 0 || len(s.entries) <= s.maxEntries {
		return
	}
	// Score every live entry and evict the lowest-scoring tail until
	// we're back inside the cap. Linear scan; cache sizes are bounded
	// (typically <10k entries).
	type scored struct {
		key   string
		score float64
	}
	all := make([]scored, 0, len(s.entries))
	for k, rec := range s.entries {
		all = append(all, scored{key: k, score: s.vaacScore(rec, now)})
	}
	// Selection-sort the worst N: cheaper than full sort when len(s.entries)
	// is large but only a few entries need eviction.
	toEvict := len(s.entries) - s.maxEntries
	for i := 0; i < toEvict; i++ {
		minIdx := i
		for j := i + 1; j < len(all); j++ {
			if all[j].score < all[minIdx].score {
				minIdx = j
			}
		}
		all[i], all[minIdx] = all[minIdx], all[i]
		delete(s.entries, all[i].key)
		s.stats.Evictions++
	}
}

// vaacScore implements (frequency × value) / (recency × freshness_decay).
// Higher = keep; lower = evict. Caller must hold s.mu.
func (s *SemanticCache) vaacScore(rec *semanticEntry, now time.Time) float64 {
	frequency := float64(rec.hitCount + 1) // +1 so brand-new entries aren't 0
	value := float64(len(rec.value) + 1)
	recency := now.Sub(rec.lastAccess).Seconds() + 1.0
	age := now.Sub(rec.insertedAt).Seconds()
	freshnessDecay := 1.0
	ttl, ok := s.perOpTTL[rec.opID]
	if !ok {
		ttl = s.defaultTTL
	}
	if ttl > 0 {
		// freshness_decay grows linearly from 1.0 at insert to 2.0 at TTL.
		freshnessDecay = 1.0 + (age / ttl.Seconds())
	}
	// Guard against arithmetic edge cases (recency=0 already pre-bumped).
	score := (frequency * value) / (recency * freshnessDecay)
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0
	}
	return score
}
