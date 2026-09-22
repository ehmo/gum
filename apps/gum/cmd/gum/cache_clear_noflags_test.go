package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/cache"
)

// seedHTTPCache writes one §10.2 entry for opID into the profile cache dir and
// returns its key.
func seedHTTPCache(t *testing.T, dir, opID string) string {
	t.Helper()
	c, err := cache.Open(cache.BBoltConfig{Path: filepath.Join(dir, cache.HTTPCacheBoltFile)})
	if err != nil {
		t.Fatalf("open http cache: %v", err)
	}
	defer func() { _ = c.Close() }()
	key := cache.HTTPKey(opID, "v1", `{"id":"x"}`, "fp")
	hc := cache.NewHTTPCache(cache.BoltHTTPStore{C: c})
	if err := hc.Store(key, cache.HTTPEntry{ETag: `W/"1"`, Body: []byte(`{"a":1}`), Format: "json", OpID: opID}); err != nil {
		t.Fatalf("store entry: %v", err)
	}
	return key
}

// profileCacheDir is <XDG_CACHE_HOME>/gum/<profile> for the default profile.
func profileCacheDir(root string) string {
	return filepath.Join(root, "gum", "default")
}

// TestCacheClearNoFlagsClearsTheHTTPStore pins the
// `!bakFlag && !expiredFlag` arm of newCacheClearCmd. §10.2 entries carry no
// TTL and nothing evicts them, so a bare `gum cache clear` is the only path
// that reclaims the store; it must report how many entries it removed.
func TestCacheClearNoFlagsClearsTheHTTPStore(t *testing.T) {
	root := withTempCacheRootCLI(t)
	seedHTTPCache(t, profileCacheDir(root), "gmail.users.messages.list")

	out, err := runCLI(t, "cache", "clear")
	if err != nil {
		t.Fatalf("gum cache clear (no flags): %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout not JSON: %v\nstdout=%q", err, out)
	}
	if cleared, _ := got["cleared"].(bool); !cleared {
		t.Errorf("cleared=%v; want true", got["cleared"])
	}
	if n, _ := got["http_entries_removed"].(float64); n != 1 {
		t.Errorf("http_entries_removed=%v; want 1", got["http_entries_removed"])
	}
	if note, _ := got["note"].(string); note == "" {
		t.Error("note empty; want the sentence naming which caches were cleared")
	}
}

// TestCacheClearPatternClearsOnlyMatchingOps pins §2031's instruction that a
// caller re-shapes a 304'd resource by clearing its entry with
// `gum cache clear <pattern>`. A pattern that names one API must leave every
// other API's validators in place.
func TestCacheClearPatternClearsOnlyMatchingOps(t *testing.T) {
	root := withTempCacheRootCLI(t)
	dir := profileCacheDir(root)
	seedHTTPCache(t, dir, "gmail.users.messages.list")
	keptKey := seedHTTPCache(t, dir, "drive.files.list")

	out, err := runCLI(t, "cache", "clear", "gmail.*")
	if err != nil {
		t.Fatalf("gum cache clear gmail.*: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout not JSON: %v\nstdout=%q", err, out)
	}
	if n, _ := got["http_entries_removed"].(float64); n != 1 {
		t.Fatalf("http_entries_removed=%v; want 1", got["http_entries_removed"])
	}

	c, err := cache.Open(cache.BBoltConfig{Path: filepath.Join(dir, cache.HTTPCacheBoltFile)})
	if err != nil {
		t.Fatalf("reopen http cache: %v", err)
	}
	defer func() { _ = c.Close() }()
	if _, ok := cache.NewHTTPCache(cache.BoltHTTPStore{C: c}).Lookup(keptKey); !ok {
		t.Error("drive.files.list entry missing; a gmail.* pattern must not clear it")
	}
}

// TestCacheStatsReportsStoredHTTPEntries pins the §3003 http counters against
// a store on disk. Hits and misses stay zero because they are per-process and
// this process dispatched nothing; entries and bytes are readable, so they
// must be read rather than reported as zero.
func TestCacheStatsReportsStoredHTTPEntries(t *testing.T) {
	root := withTempCacheRootCLI(t)
	seedHTTPCache(t, profileCacheDir(root), "gmail.users.messages.list")

	out, err := runCLI(t, "cache", "stats")
	if err != nil {
		t.Fatalf("gum cache stats: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout not JSON: %v\nstdout=%q", err, out)
	}
	http, _ := got["http"].(map[string]any)
	if entries, _ := http["entries"].(float64); entries != 1 {
		t.Errorf("http.entries=%v; want 1", http["entries"])
	}
	if bytes, _ := http["bytes"].(float64); bytes <= 0 {
		t.Errorf("http.bytes=%v; want > 0", http["bytes"])
	}
}
