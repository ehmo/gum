package main

// Tests for `gum cache stats --format=json`, `gum cache clear --bak`,
// and `gum cache clear --expired` CLI surface (spec §10.2).
//
// Environment isolation: every test redirects XDG_CACHE_HOME (and
// XDG_CONFIG_HOME) to t.TempDir() so it cannot touch the developer's real
// ~/.cache/gum tree.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/ehmo/gum/internal/cache"
)

// withTempCacheRootCLI redirects XDG_CACHE_HOME (and XDG_CONFIG_HOME for
// isolation) to a t.TempDir() for the duration of the test. Returns the
// XDG_CACHE_HOME path.
func withTempCacheRootCLI(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return root
}

// cacheStatsSchema is the CacheStatsResult JSON Schema inlined from
// docs/spec.md lines 3003-3055.
const cacheStatsSchema = `{
  "type": "object",
  "required": ["semantic", "http", "prompt", "audit_broken"],
  "properties": {
    "semantic": {
      "type": "object",
      "required": ["hits", "misses", "evictions", "entries", "bytes"],
      "properties": {
        "hits":      {"type": "integer", "minimum": 0},
        "misses":    {"type": "integer", "minimum": 0},
        "evictions": {"type": "integer", "minimum": 0},
        "entries":   {"type": "integer", "minimum": 0},
        "bytes":     {"type": "integer", "minimum": 0}
      },
      "additionalProperties": false
    },
    "http": {
      "type": "object",
      "required": ["hits", "misses", "entries", "bytes"],
      "properties": {
        "hits":    {"type": "integer", "minimum": 0},
        "misses":  {"type": "integer", "minimum": 0},
        "entries": {"type": "integer", "minimum": 0},
        "bytes":   {"type": "integer", "minimum": 0}
      },
      "additionalProperties": false
    },
    "prompt": {
      "type": "object",
      "required": ["supported", "hits_estimate"],
      "properties": {
        "supported":     {"type": "boolean"},
        "hits_estimate": {"type": ["integer", "null"], "minimum": 0}
      },
      "additionalProperties": false
    },
    "audit_broken": {
      "type": "boolean"
    }
  },
  "additionalProperties": false
}`

// compileCacheStatsSchema parses and resolves the inline CacheStatsResult schema.
func compileCacheStatsSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()
	var s jsonschema.Schema
	if err := json.Unmarshal([]byte(cacheStatsSchema), &s); err != nil {
		t.Fatalf("compileCacheStatsSchema: parse: %v", err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("compileCacheStatsSchema: resolve: %v", err)
	}
	return rs
}

// TestCacheStatsFormatJSONValidatesAgainstSchema asserts that
// `gum cache stats --format=json` emits a JSON object that validates against
// the spec §3003 CacheStatsResult schema.
func TestCacheStatsFormatJSONValidatesAgainstSchema(t *testing.T) {
	withTempCacheRootCLI(t)

	out, err := runCLI(t, "cache", "stats", "--format=json")
	if err != nil {
		t.Fatalf("gum cache stats --format=json: unexpected error: %v", err)
	}

	var doc any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, out)
	}

	rs := compileCacheStatsSchema(t)
	if err := rs.Validate(doc); err != nil {
		t.Errorf("output does not validate against CacheStatsResult schema: %v\nstdout: %s", err, out)
	}
}

// TestCacheStatsFormatJSONHasRequiredKeys asserts the EXACT top-level and
// nested key sets in `gum cache stats --format=json` output.
func TestCacheStatsFormatJSONHasRequiredKeys(t *testing.T) {
	withTempCacheRootCLI(t)

	out, err := runCLI(t, "cache", "stats", "--format=json")
	if err != nil {
		t.Fatalf("gum cache stats --format=json: unexpected error: %v", err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &top); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\nstdout: %q", err, out)
	}

	// Exact top-level keys.
	wantTopKeys := []string{"semantic", "http", "prompt", "audit_broken"}
	for _, k := range wantTopKeys {
		if _, ok := top[k]; !ok {
			t.Errorf("missing top-level key %q", k)
		}
	}
	if len(top) != len(wantTopKeys) {
		t.Errorf("top-level key count: got %d, want %d (keys: %v)", len(top), len(wantTopKeys), keyNames(top))
	}

	// semantic sub-keys.
	var semantic map[string]json.RawMessage
	if err := json.Unmarshal(top["semantic"], &semantic); err != nil {
		t.Fatalf("semantic is not an object: %v", err)
	}
	wantSemantic := []string{"hits", "misses", "evictions", "entries", "bytes"}
	for _, k := range wantSemantic {
		if _, ok := semantic[k]; !ok {
			t.Errorf("semantic: missing key %q", k)
		}
	}
	if len(semantic) != len(wantSemantic) {
		t.Errorf("semantic key count: got %d, want %d (keys: %v)", len(semantic), len(wantSemantic), keyNames(semantic))
	}

	// http sub-keys.
	var httpObj map[string]json.RawMessage
	if err := json.Unmarshal(top["http"], &httpObj); err != nil {
		t.Fatalf("http is not an object: %v", err)
	}
	wantHTTP := []string{"hits", "misses", "entries", "bytes"}
	for _, k := range wantHTTP {
		if _, ok := httpObj[k]; !ok {
			t.Errorf("http: missing key %q", k)
		}
	}
	if len(httpObj) != len(wantHTTP) {
		t.Errorf("http key count: got %d, want %d (keys: %v)", len(httpObj), len(wantHTTP), keyNames(httpObj))
	}

	// prompt sub-keys.
	var prompt map[string]json.RawMessage
	if err := json.Unmarshal(top["prompt"], &prompt); err != nil {
		t.Fatalf("prompt is not an object: %v", err)
	}
	wantPrompt := []string{"supported", "hits_estimate"}
	for _, k := range wantPrompt {
		if _, ok := prompt[k]; !ok {
			t.Errorf("prompt: missing key %q", k)
		}
	}
	if len(prompt) != len(wantPrompt) {
		t.Errorf("prompt key count: got %d, want %d (keys: %v)", len(prompt), len(wantPrompt), keyNames(prompt))
	}

	// audit_broken must be a JSON boolean.
	var auditBroken bool
	if err := json.Unmarshal(top["audit_broken"], &auditBroken); err != nil {
		t.Errorf("audit_broken is not a boolean: %v (raw: %s)", err, top["audit_broken"])
	}
}

// keyNames returns the key names of a map[string]json.RawMessage for error messages.
func keyNames(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestCacheStatsDefaultMatchesJSONFormat pins the unified-schema contract
// (review gum-oqer): `gum cache stats` (no --format) and `--format=json` must
// emit the same §3003 envelope, so adding the flag never changes the shape a
// script already parses. The old v0.1.0 placeholder ({version,note,...}) is
// retired.
func TestCacheStatsDefaultMatchesJSONFormat(t *testing.T) {
	withTempCacheRootCLI(t)

	bare, err := runCLI(t, "cache", "stats")
	if err != nil {
		t.Fatalf("gum cache stats (no --format): unexpected error: %v", err)
	}
	withFlag, err := runCLI(t, "cache", "stats", "--format=json")
	if err != nil {
		t.Fatalf("gum cache stats --format=json: unexpected error: %v", err)
	}

	var bareObj, flagObj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(bare), &bareObj); err != nil {
		t.Fatalf("bare stats is not JSON: %v; got %q", err, bare)
	}
	if err := json.Unmarshal([]byte(withFlag), &flagObj); err != nil {
		t.Fatalf("--format=json stats is not JSON: %v; got %q", err, withFlag)
	}
	for _, k := range []string{"semantic", "http", "prompt", "audit_broken"} {
		if _, ok := bareObj[k]; !ok {
			t.Errorf("bare stats missing key %q; got %q", k, bare)
		}
	}
	if len(bareObj) != len(flagObj) {
		t.Errorf("default and --format=json key counts differ: %d vs %d", len(bareObj), len(flagObj))
	}
}

// TestCacheClearBakRemovesFileWhenPresent asserts that `gum cache clear --bak`
// removes <XDG_CACHE_HOME>/gum/default/http.db.bak when it exists and reports
// removed_bak==true.
func TestCacheClearBakRemovesFileWhenPresent(t *testing.T) {
	root := withTempCacheRootCLI(t)

	bakPath := filepath.Join(root, "gum", "default", "http.db.bak")
	if err := os.MkdirAll(filepath.Dir(bakPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(bakPath, []byte("legacy"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, err := runCLI(t, "cache", "clear", "--bak")
	if err != nil {
		t.Fatalf("gum cache clear --bak: unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, out)
	}

	removedBak, ok := result["removed_bak"].(bool)
	if !ok {
		t.Errorf("removed_bak missing or not bool: %v", result["removed_bak"])
	} else if !removedBak {
		t.Errorf("removed_bak: got false, want true")
	}

	pathVal, _ := result["path"].(string)
	if !strings.HasSuffix(pathVal, filepath.Join("gum", "default", "http.db.bak")) {
		t.Errorf("path %q does not end with gum/default/http.db.bak", pathVal)
	}

	if _, statErr := os.Stat(bakPath); !os.IsNotExist(statErr) {
		t.Errorf("http.db.bak still exists after clear --bak")
	}
}

// TestCacheClearBakWhenAbsent asserts that `gum cache clear --bak` succeeds
// and reports removed_bak==false when the .bak file does not exist.
func TestCacheClearBakWhenAbsent(t *testing.T) {
	root := withTempCacheRootCLI(t)

	out, err := runCLI(t, "cache", "clear", "--bak")
	if err != nil {
		t.Fatalf("gum cache clear --bak (absent): unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, out)
	}

	removedBak, ok := result["removed_bak"].(bool)
	if !ok {
		t.Errorf("removed_bak missing or not bool: %v", result["removed_bak"])
	} else if removedBak {
		t.Errorf("removed_bak: got true, want false")
	}

	wantPath := filepath.Join(root, "gum", "default", "http.db.bak")
	pathVal, _ := result["path"].(string)
	if pathVal != wantPath {
		t.Errorf("path: got %q, want %q", pathVal, wantPath)
	}
}

// TestCacheClearBakHonorsProfile asserts that `gum --profile=team-a cache clear --bak`
// removes team-a's bak file without touching default's bak file.
func TestCacheClearBakHonorsProfile(t *testing.T) {
	root := withTempCacheRootCLI(t)

	teamABak := filepath.Join(root, "gum", "team-a", "http.db.bak")
	defaultBak := filepath.Join(root, "gum", "default", "http.db.bak")
	for _, p := range []string{teamABak, defaultBak} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte("bak"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}

	_, err := runCLI(t, "--profile=team-a", "cache", "clear", "--bak")
	if err != nil {
		t.Fatalf("gum --profile=team-a cache clear --bak: unexpected error: %v", err)
	}

	if _, statErr := os.Stat(teamABak); !os.IsNotExist(statErr) {
		t.Errorf("team-a http.db.bak still exists after clear --bak")
	}
	if _, statErr := os.Stat(defaultBak); statErr != nil {
		t.Errorf("default http.db.bak unexpectedly removed or inaccessible: %v", statErr)
	}
}

// TestCacheClearExpiredRemovesExpiredEntries asserts that `gum cache clear --expired`
// evicts TTL-expired entries and reports the count.
func TestCacheClearExpiredRemovesExpiredEntries(t *testing.T) {
	root := withTempCacheRootCLI(t)

	cacheDir := filepath.Join(root, "gum", "default")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cachePath := filepath.Join(cacheDir, "cache.db")

	c, err := cache.Open(cache.BBoltConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	if err := c.Set("expired-key", []byte("payload-a"), 1*time.Nanosecond); err != nil {
		t.Fatalf("Set expired-key: %v", err)
	}
	if err := c.Set("live-key", []byte("payload-b"), 5*time.Minute); err != nil {
		t.Fatalf("Set live-key: %v", err)
	}
	time.Sleep(20 * time.Millisecond) // ensure nanosecond TTL has elapsed
	if err := c.Close(); err != nil {
		t.Fatalf("c.Close: %v", err)
	}

	out, err := runCLI(t, "cache", "clear", "--expired")
	if err != nil {
		t.Fatalf("gum cache clear --expired: unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, out)
	}

	expiredRemoved, ok := result["expired_removed"].(float64) // JSON numbers decode as float64
	if !ok {
		t.Fatalf("expired_removed missing or wrong type: %v", result["expired_removed"])
	}
	if int(expiredRemoved) != 1 {
		t.Errorf("expired_removed: got %d, want 1", int(expiredRemoved))
	}

	// Re-open and verify live-key survives, expired-key does not.
	c2, err := cache.Open(cache.BBoltConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("cache.Open (verify): %v", err)
	}
	defer c2.Close() //nolint:errcheck

	if _, ok := c2.Get("live-key"); !ok {
		t.Errorf("live-key unexpectedly missing after clear --expired")
	}
	if _, ok := c2.Get("expired-key"); ok {
		t.Errorf("expired-key still present after clear --expired")
	}
}

// TestCacheClearExpiredZeroWhenNoCacheFile asserts that `gum cache clear --expired`
// succeeds with expired_removed==0 when cache.db does not exist, and does NOT
// create a spurious cache.db.
func TestCacheClearExpiredZeroWhenNoCacheFile(t *testing.T) {
	root := withTempCacheRootCLI(t)

	out, err := runCLI(t, "cache", "clear", "--expired")
	if err != nil {
		t.Fatalf("gum cache clear --expired (no cache.db): unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, out)
	}

	expiredRemoved, ok := result["expired_removed"].(float64)
	if !ok {
		t.Fatalf("expired_removed missing or wrong type: %v", result["expired_removed"])
	}
	if int(expiredRemoved) != 0 {
		t.Errorf("expired_removed: got %d, want 0", int(expiredRemoved))
	}

	spurious := filepath.Join(root, "gum", "default", "cache.db")
	if _, statErr := os.Stat(spurious); !os.IsNotExist(statErr) {
		t.Errorf("cache.db was created spuriously at %s", spurious)
	}
}

// TestCacheClearBakAndExpiredCombined asserts that `gum cache clear --bak --expired`
// emits a single JSON object with both removed_bak==true and expired_removed==1.
func TestCacheClearBakAndExpiredCombined(t *testing.T) {
	root := withTempCacheRootCLI(t)

	// Pre-create the .bak file.
	bakPath := filepath.Join(root, "gum", "default", "http.db.bak")
	if err := os.MkdirAll(filepath.Dir(bakPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(bakPath, []byte("legacy"), 0o600); err != nil {
		t.Fatalf("WriteFile bak: %v", err)
	}

	// Pre-populate cache.db with one expired entry.
	cachePath := filepath.Join(root, "gum", "default", "cache.db")
	c, err := cache.Open(cache.BBoltConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	if err := c.Set("expired-key", []byte("payload"), 1*time.Nanosecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatalf("c.Close: %v", err)
	}

	out, err := runCLI(t, "cache", "clear", "--bak", "--expired")
	if err != nil {
		t.Fatalf("gum cache clear --bak --expired: unexpected error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, out)
	}

	removedBak, ok := result["removed_bak"].(bool)
	if !ok || !removedBak {
		t.Errorf("removed_bak: got %v, want true", result["removed_bak"])
	}

	expiredRemoved, ok := result["expired_removed"].(float64)
	if !ok {
		t.Fatalf("expired_removed missing or wrong type: %v", result["expired_removed"])
	}
	if int(expiredRemoved) != 1 {
		t.Errorf("expired_removed: got %d, want 1", int(expiredRemoved))
	}
}

// TestCacheMigrateFreshBootstrap covers `gum cache migrate` when neither
// http.db nor http-wal.db exists: the CLI must create http-wal.db, write
// the sentinel, and emit the spec §10.2 result envelope.
func TestCacheMigrateFreshBootstrap(t *testing.T) {
	root := withTempCacheRootCLI(t)

	out, err := runCLI(t, "cache", "migrate")
	if err != nil {
		t.Fatalf("gum cache migrate (fresh): unexpected error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %q", err, out)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Errorf("ok=false on fresh bootstrap; result=%v", result)
	}
	if written, _ := result["sentinel_written"].(bool); !written {
		t.Errorf("sentinel_written=false; want true on fresh bootstrap")
	}
	walPath := filepath.Join(root, "gum", "default", "http-wal.db")
	if _, statErr := os.Stat(walPath); statErr != nil {
		t.Errorf("http-wal.db not created at %q: %v", walPath, statErr)
	}
}

// TestCacheMigrateBoltToWAL covers the bolt-present branch: the CLI must
// migrate entries, write sentinel, and rename http.db to http.db.bak.
func TestCacheMigrateBoltToWAL(t *testing.T) {
	root := withTempCacheRootCLI(t)

	profileDir := filepath.Join(root, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Seed bolt via the cache package's bbolt API directly through the
	// migrate test helper would create a recursive import; we instead use
	// cache.Open which creates the on-disk bbolt structure used by the
	// migration path (with the standard cacheBucket bucket).
	c, err := cache.Open(cache.BBoltConfig{Path: filepath.Join(profileDir, "http.db")})
	if err != nil {
		t.Fatalf("seed bbolt: %v", err)
	}
	if err := c.Set("seed-key", []byte("seed-val"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	out, err := runCLI(t, "cache", "migrate")
	if err != nil {
		t.Fatalf("gum cache migrate: unexpected error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout JSON parse: %v\nstdout: %q", err, out)
	}
	if entries, _ := result["entries_migrated"].(float64); int(entries) < 1 {
		t.Errorf("entries_migrated=%v; want >=1", result["entries_migrated"])
	}
	if renamed, _ := result["bak_renamed"].(bool); !renamed {
		t.Errorf("bak_renamed=false; want true")
	}
	if _, statErr := os.Stat(filepath.Join(profileDir, "http.db.bak")); statErr != nil {
		t.Errorf("http.db.bak missing after migrate: %v", statErr)
	}
}

// TestCacheMigrateForceResolvesAmbiguity exercises the --force branch:
// when both http.db and an un-sentineled http-wal.db exist, --force
// must discard the WAL and re-migrate from bolt cleanly.
func TestCacheMigrateForceResolvesAmbiguity(t *testing.T) {
	root := withTempCacheRootCLI(t)
	profileDir := filepath.Join(root, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	c, _ := cache.Open(cache.BBoltConfig{Path: filepath.Join(profileDir, "http.db")})
	_ = c.Set("k", []byte("v"), 0)
	_ = c.Close()
	s, err := cache.OpenSQLiteWAL(cache.SQLiteConfig{Path: filepath.Join(profileDir, "http-wal.db")})
	if err != nil {
		t.Fatalf("OpenSQLiteWAL: %v", err)
	}
	_ = s.Close()

	out, err := runCLI(t, "cache", "migrate", "--force")
	if err != nil {
		t.Fatalf("gum cache migrate --force: unexpected error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout JSON parse: %v\nstdout: %q", err, out)
	}
	if ok, _ := result["ok"].(bool); !ok {
		t.Errorf("ok=false with --force; result=%v", result)
	}
}

// TestCacheMigrateHomeFallback exercises the $HOME branch when
// XDG_CACHE_HOME is unset — the migrate command must still resolve a
// per-profile cache dir under $HOME/.cache/gum/<profile>.
func TestCacheMigrateHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := runCLI(t, "cache", "migrate")
	if err != nil {
		t.Fatalf("gum cache migrate ($HOME fallback): unexpected error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout JSON parse: %v\nstdout: %q", err, out)
	}
	wantDir := filepath.Join(home, ".cache", "gum", "default")
	if got, _ := result["profile_dir"].(string); got != wantDir {
		t.Errorf("profile_dir=%q; want %q", got, wantDir)
	}
}

// TestCacheMigrateRsyncAmbiguityWithoutForce confirms the CLI surfaces
// the spec §10.2 rsync-ambiguity error as a JSON envelope with
// ok=false + error=RSYNC_AMBIGUITY (no shell error so scripts can
// branch on the envelope).
func TestCacheMigrateRsyncAmbiguityWithoutForce(t *testing.T) {
	root := withTempCacheRootCLI(t)
	profileDir := filepath.Join(root, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Seed both files; wal lacks sentinel.
	c, _ := cache.Open(cache.BBoltConfig{Path: filepath.Join(profileDir, "http.db")})
	_ = c.Close()
	s, err := cache.OpenSQLiteWAL(cache.SQLiteConfig{Path: filepath.Join(profileDir, "http-wal.db")})
	if err != nil {
		t.Fatalf("OpenSQLiteWAL: %v", err)
	}
	_ = s.Close()

	out, err := runCLI(t, "cache", "migrate")
	if err != nil {
		t.Fatalf("gum cache migrate (ambiguous): unexpected error: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout JSON parse: %v\nstdout: %q", err, out)
	}
	if ok, _ := result["ok"].(bool); ok {
		t.Errorf("ok=true on ambiguity; want false")
	}
	if got, _ := result["error"].(string); got != "RSYNC_AMBIGUITY" {
		t.Errorf("error=%q; want RSYNC_AMBIGUITY", got)
	}
}
