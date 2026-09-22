package cache_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/cache"
)

// errStore is an HTTPStore whose halves fail, for the arms a working backend
// cannot reach. It implements neither HTTPPurger nor HTTPWalker, so it also
// covers the "store cannot reclaim or report" branches.
type errStore struct {
	getErr error
	setErr error
}

func (s errStore) Get(string) ([]byte, bool, error)        { return nil, false, s.getErr }
func (s errStore) Set(string, []byte, time.Duration) error { return s.setErr }

// mapStore is a minimal in-memory HTTPStore, used where the test needs a
// backend that stores but cannot purge.
type mapStore map[string][]byte

func (m mapStore) Get(key string) ([]byte, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}

func (m mapStore) Set(key string, value []byte, _ time.Duration) error {
	m[key] = append([]byte(nil), value...)
	return nil
}

func newBoltStore(t *testing.T) cache.BoltHTTPStore {
	t.Helper()
	c, err := cache.Open(cache.BBoltConfig{Path: filepath.Join(t.TempDir(), "http.db")})
	if err != nil {
		t.Fatalf("open bbolt: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return cache.BoltHTTPStore{C: c}
}

// TestHTTPKeyComponents pins the §10.2 key: all four components are part of
// it, so no two calls that differ in any of them can share a validator.
func TestHTTPKeyComponents(t *testing.T) {
	t.Parallel()

	base := cache.HTTPKey("op", "v1", `{"a":1}`, "fp")
	if same := cache.HTTPKey("op", "v1", `{"a":1}`, "fp"); same != base {
		t.Error("the same four components must give the same key")
	}
	for name, got := range map[string]string{
		"op_id":     cache.HTTPKey("other", "v1", `{"a":1}`, "fp"),
		"variant":   cache.HTTPKey("op", "v2", `{"a":1}`, "fp"),
		"args":      cache.HTTPKey("op", "v1", `{"a":2}`, "fp"),
		"principal": cache.HTTPKey("op", "v1", `{"a":1}`, "other"),
	} {
		if got == base {
			t.Errorf("changing %s did not change the key", name)
		}
	}
	// The components are NUL-joined, so a value containing the separator
	// cannot impersonate the next component.
	if cache.HTTPKey("a\x00b", "", "", "") == cache.HTTPKey("a", "b", "", "") {
		t.Error("component boundaries are forgeable")
	}
}

// TestHTTPCacheRoundTrip covers store, lookup, and the counters.
func TestHTTPCacheRoundTrip(t *testing.T) {
	t.Parallel()

	c := cache.NewHTTPCache(newBoltStore(t))
	key := cache.HTTPKey("gmail.users.messages.list", "v1", "{}", "fp")

	if _, ok := c.Lookup(key); ok {
		t.Fatal("an empty cache must miss")
	}
	want := cache.HTTPEntry{ETag: `W/"1"`, Body: []byte(`{"messages":[]}`), Format: "json", OpID: "gmail.users.messages.list"}
	if err := c.Store(key, want); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, ok := c.Lookup(key)
	if !ok {
		t.Fatal("a stored entry must be found")
	}
	if got.ETag != want.ETag || string(got.Body) != string(want.Body) || got.Format != want.Format || got.OpID != want.OpID {
		t.Errorf("round trip changed the entry: %+v", got)
	}
	if s := c.Stats(); s.Hits != 1 || s.Misses != 1 || s.Stores != 1 {
		t.Errorf("stats = %+v; want 1 hit, 1 miss, 1 store", s)
	}
}

// TestHTTPCacheDegradedArms covers every path where the cache answers "miss"
// rather than failing the dispatch.
func TestHTTPCacheDegradedArms(t *testing.T) {
	t.Parallel()

	t.Run("a nil store yields a nil cache whose methods are no-ops", func(t *testing.T) {
		t.Parallel()

		c := cache.NewHTTPCache(nil)
		if c != nil {
			t.Fatal("NewHTTPCache(nil) must return nil")
		}
		if _, ok := c.Lookup("k"); ok {
			t.Error("a nil cache must miss")
		}
		if err := c.Store("k", cache.HTTPEntry{ETag: "x"}); err != nil {
			t.Errorf("a nil cache must not fail a store: %v", err)
		}
		if s := c.Stats(); s != (cache.HTTPStats{}) {
			t.Errorf("stats = %+v; want the zero value", s)
		}
	})

	t.Run("a backend read error is a miss", func(t *testing.T) {
		t.Parallel()

		c := cache.NewHTTPCache(errStore{getErr: errors.New("disk gone")})
		if _, ok := c.Lookup("k"); ok {
			t.Error("a read error must not produce a validator")
		}
		if s := c.Stats(); s.Misses != 1 {
			t.Errorf("misses = %d; want 1", s.Misses)
		}
	})

	t.Run("an undecodable record is a miss", func(t *testing.T) {
		t.Parallel()

		store := mapStore{"k": []byte("not json")}
		if _, ok := cache.NewHTTPCache(store).Lookup("k"); ok {
			t.Error("a record this build cannot decode must revalidate nothing")
		}
	})

	t.Run("a stored entry with no validator is a miss", func(t *testing.T) {
		t.Parallel()

		payload, err := json.Marshal(cache.HTTPEntry{Body: []byte("{}")})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if _, ok := cache.NewHTTPCache(mapStore{"k": payload}).Lookup("k"); ok {
			t.Error("an entry without an ETag cannot make a conditional request")
		}
	})

	t.Run("storing without a validator writes nothing", func(t *testing.T) {
		t.Parallel()

		store := mapStore{}
		c := cache.NewHTTPCache(store)
		if err := c.Store("k", cache.HTTPEntry{Body: []byte("{}")}); err != nil {
			t.Fatalf("store: %v", err)
		}
		if len(store) != 0 {
			t.Errorf("store holds %d entries; want 0", len(store))
		}
		if s := c.Stats(); s.Stores != 0 {
			t.Errorf("stores = %d; want 0", s.Stores)
		}
	})

	t.Run("a backend write error reaches the caller", func(t *testing.T) {
		t.Parallel()

		c := cache.NewHTTPCache(errStore{setErr: errors.New("read-only fs")})
		if err := c.Store("k", cache.HTTPEntry{ETag: "x"}); err == nil {
			t.Error("a failed write must not be reported as stored")
		}
		if s := c.Stats(); s.Stores != 0 {
			t.Errorf("stores = %d; want 0", s.Stores)
		}
	})
}

// TestClearHTTP covers §2031's reclaim path: the glob matches op_id, and an
// entry a pattern cannot name survives a targeted clear.
func TestClearHTTP(t *testing.T) {
	t.Parallel()

	seed := func(t *testing.T) (cache.BoltHTTPStore, map[string]string) {
		t.Helper()
		store := newBoltStore(t)
		c := cache.NewHTTPCache(store)
		keys := map[string]string{}
		for _, opID := range []string{"gmail.users.messages.list", "gmail.users.labels.list", "drive.files.list"} {
			k := cache.HTTPKey(opID, "v1", "{}", "fp")
			keys[opID] = k
			if err := c.Store(k, cache.HTTPEntry{ETag: `W/"` + opID + `"`, Body: []byte("{}"), Format: "json", OpID: opID}); err != nil {
				t.Fatalf("seed %s: %v", opID, err)
			}
		}
		return store, keys
	}

	t.Run("a glob clears one API", func(t *testing.T) {
		t.Parallel()

		store, keys := seed(t)
		n, err := cache.ClearHTTP(store, "gmail.*")
		if err != nil {
			t.Fatalf("clear: %v", err)
		}
		if n != 2 {
			t.Errorf("cleared %d; want 2", n)
		}
		if _, ok := cache.NewHTTPCache(store).Lookup(keys["drive.files.list"]); !ok {
			t.Error("a non-matching op was cleared")
		}
	})

	t.Run("an empty pattern clears everything", func(t *testing.T) {
		t.Parallel()

		store, keys := seed(t)
		n, err := cache.ClearHTTP(store, "")
		if err != nil {
			t.Fatalf("clear: %v", err)
		}
		if n != 3 {
			t.Errorf("cleared %d; want 3", n)
		}
		if _, ok := cache.NewHTTPCache(store).Lookup(keys["drive.files.list"]); ok {
			t.Error("an unqualified clear left an entry behind")
		}
	})

	t.Run("a bad pattern is an error, not a wipe", func(t *testing.T) {
		t.Parallel()

		store, keys := seed(t)
		if _, err := cache.ClearHTTP(store, "[bad"); err == nil {
			t.Fatal("a malformed glob must be rejected")
		}
		if _, ok := cache.NewHTTPCache(store).Lookup(keys["drive.files.list"]); !ok {
			t.Error("a rejected pattern still deleted entries")
		}
	})

	t.Run("an unnameable entry survives a targeted clear", func(t *testing.T) {
		t.Parallel()

		store := newBoltStore(t)
		// An entry from before op_id was recorded, and a row this build
		// cannot decode. Neither can be named by a glob.
		anon, err := json.Marshal(cache.HTTPEntry{ETag: `W/"x"`, Body: []byte("{}")})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := store.Set("anon", anon, 0); err != nil {
			t.Fatalf("seed anon: %v", err)
		}
		if err := store.Set("corrupt", []byte("not json"), 0); err != nil {
			t.Fatalf("seed corrupt: %v", err)
		}
		n, err := cache.ClearHTTP(store, "gmail.*")
		if err != nil {
			t.Fatalf("clear: %v", err)
		}
		if n != 0 {
			t.Errorf("cleared %d; a targeted clear must not delete what it cannot name", n)
		}
		// The unqualified clear is the escape hatch for exactly those rows.
		if n, err := cache.ClearHTTP(store, ""); err != nil || n != 2 {
			t.Errorf("clear all = %d, %v; want 2, nil", n, err)
		}
	})

	t.Run("a store that cannot purge reports zero", func(t *testing.T) {
		t.Parallel()

		n, err := cache.ClearHTTP(mapStore{"k": []byte("{}")}, "")
		if err != nil || n != 0 {
			t.Errorf("clear = %d, %v; want 0, nil", n, err)
		}
	})
}

// TestMeasureHTTP covers `gum cache stats` sizing a store on disk.
func TestMeasureHTTP(t *testing.T) {
	t.Parallel()

	store := newBoltStore(t)
	c := cache.NewHTTPCache(store)
	for _, opID := range []string{"a.list", "b.list"} {
		if err := c.Store(cache.HTTPKey(opID, "v1", "{}", "fp"), cache.HTTPEntry{ETag: "x", Body: []byte(`{"k":"v"}`), Format: "json", OpID: opID}); err != nil {
			t.Fatalf("seed %s: %v", opID, err)
		}
	}
	u, err := cache.MeasureHTTP(store)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if u.Entries != 2 {
		t.Errorf("entries = %d; want 2", u.Entries)
	}
	if u.Bytes <= 0 {
		t.Errorf("bytes = %d; want a positive size", u.Bytes)
	}

	// A backend that cannot walk reports what it truthfully knows.
	empty, err := cache.MeasureHTTP(mapStore{"k": []byte("{}")})
	if err != nil {
		t.Fatalf("measure unwalkable: %v", err)
	}
	if empty != (cache.HTTPUsage{}) {
		t.Errorf("usage = %+v; want the zero value", empty)
	}
}

// TestBoltDeleteWhereAndForEachNilArgs pins the guard clauses: a nil callback
// is a no-op rather than a panic, so a caller cannot take the store down.
func TestBoltDeleteWhereAndForEachNilArgs(t *testing.T) {
	t.Parallel()

	store := newBoltStore(t)
	if err := store.Set("k", []byte("{}"), 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	n, err := store.C.DeleteWhere(nil)
	if err != nil || n != 0 {
		t.Errorf("DeleteWhere(nil) = %d, %v; want 0, nil", n, err)
	}
	if err := store.C.ForEachPayload(nil); err != nil {
		t.Errorf("ForEachPayload(nil) = %v; want nil", err)
	}
	if _, ok, _ := store.Get("k"); !ok {
		t.Error("a nil callback deleted an entry")
	}
}

// TestSQLiteWALIsAnHTTPStore proves the second shipped backend really answers
// the HTTPStore contract, which the compile-time assertion alone cannot show.
func TestSQLiteWALIsAnHTTPStore(t *testing.T) {
	t.Parallel()

	s, err := cache.OpenSQLiteWAL(cache.SQLiteConfig{Path: filepath.Join(t.TempDir(), "http.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	c := cache.NewHTTPCache(s)
	key := cache.HTTPKey("op", "v1", "{}", "fp")
	if err := c.Store(key, cache.HTTPEntry{ETag: `W/"3"`, Body: []byte("{}"), Format: "json", OpID: "op"}); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, ok := c.Lookup(key)
	if !ok || got.ETag != `W/"3"` {
		t.Errorf("lookup = %+v, %v; want the stored validator", got, ok)
	}
}
