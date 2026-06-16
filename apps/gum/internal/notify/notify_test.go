package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.1.0", "v0.1.0", 0},
		{"v0.1.1", "v0.1.0", 1},
		{"v0.1.0", "v0.1.1", -1},
		{"v0.2.0", "v0.1.99", 1},
		{"v1.0.0", "v0.99.99", 1},
		{"0.1.0", "v0.1.0", 0},
		{"v0.1.0-rc1", "v0.1.0", 0},  // numeric prefix equal → treat as equal
		{"v0.1.1-rc1", "v0.1.0", 1},  // numeric prefix wins
		{"garbage", "v0.1.0", 0},     // unparseable → 0 (no warning)
		{"v0.1.0", "garbage", 0},
	}
	for _, c := range cases {
		got := CompareVersions(c.a, c.b)
		if got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCachePathHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	p, err := CachePath("myprofile")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/xdg-cache", "gum", "myprofile", "notify.json")
	if p != want {
		t.Errorf("CachePath = %q, want %q", p, want)
	}
}

func TestCachePathEmptyProfileDefaultsToDefault(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	p, err := CachePath("")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, filepath.Join("gum", "default", "notify.json")) {
		t.Errorf("CachePath empty profile should fall back to 'default', got %q", p)
	}
}

// TestCachePathHomeFallback covers the XDG_CACHE_HOME-unset branch:
// CachePath must derive base from $HOME/.cache when the env var is
// empty. Without this branch covered, a misconfigured developer
// environment would silently scribble notify.json into the wrong dir.
func TestCachePathHomeFallback(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	p, err := CachePath("teamA")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".cache", "gum", "teamA", "notify.json")
	if p != want {
		t.Errorf("CachePath = %q, want %q", p, want)
	}
}

// stubFetcher is an in-memory Fetcher used by MaybeNotify tests.
type stubFetcher struct {
	tag string
	err error
}

func (s stubFetcher) Latest(_ context.Context, _ string) (string, error) {
	return s.tag, s.err
}

func TestMaybeNotifyDisabledIsSilent(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var buf bytes.Buffer
	done := make(chan struct{})
	MaybeNotify(&buf, "v0.1.0", "default", false, stubFetcher{tag: "v0.2.0"}, done)
	<-done
	if buf.Len() != 0 {
		t.Errorf("disabled notifier wrote to stderr: %q", buf.String())
	}
}

func TestMaybeNotifyDevBuildIsSilent(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var buf bytes.Buffer
	done := make(chan struct{})
	MaybeNotify(&buf, "dev", "default", true, stubFetcher{tag: "v0.2.0"}, done)
	<-done
	if buf.Len() != 0 {
		t.Errorf("dev-build notifier wrote to stderr: %q", buf.String())
	}
}

func TestMaybeNotifyEmptyCurrentIsSilent(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var buf bytes.Buffer
	done := make(chan struct{})
	MaybeNotify(&buf, "", "default", true, stubFetcher{tag: "v0.2.0"}, done)
	<-done
	if buf.Len() != 0 {
		t.Errorf("empty-current notifier wrote to stderr: %q", buf.String())
	}
}

func TestMaybeNotifyFreshCacheWithNewerVersionWarns(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	// Seed the cache as freshly written 5 minutes ago.
	entry := cacheEntry{CheckedAt: time.Now().Add(-5 * time.Minute), LatestVersion: "v0.2.0"}
	data, _ := json.Marshal(entry)
	cachePath := filepath.Join(cacheRoot, "gum", "default", "notify.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	done := make(chan struct{})
	// Fetcher returns an error — must not be called because cache is fresh.
	MaybeNotify(&buf, "v0.1.0", "default", true, stubFetcher{err: errors.New("must not be called")}, done)
	<-done

	out := buf.String()
	if !strings.Contains(out, "v0.2.0") || !strings.Contains(out, "v0.1.0") {
		t.Errorf("expected warning to mention both versions; got %q", out)
	}
	if !strings.Contains(out, "[notify]") {
		t.Errorf("expected [notify] prefix; got %q", out)
	}
}

func TestMaybeNotifyFreshCacheSameVersionSilent(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	entry := cacheEntry{CheckedAt: time.Now().Add(-5 * time.Minute), LatestVersion: "v0.1.0"}
	data, _ := json.Marshal(entry)
	cachePath := filepath.Join(cacheRoot, "gum", "default", "notify.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	done := make(chan struct{})
	MaybeNotify(&buf, "v0.1.0", "default", true, stubFetcher{err: errors.New("must not be called")}, done)
	<-done
	if buf.Len() != 0 {
		t.Errorf("same-version notifier wrote to stderr: %q", buf.String())
	}
}

func TestMaybeNotifyStaleCacheTriggersAsyncRefresh(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	// Stale: CheckedAt 25 hours ago.
	entry := cacheEntry{CheckedAt: time.Now().Add(-25 * time.Hour), LatestVersion: "v0.1.0"}
	data, _ := json.Marshal(entry)
	cachePath := filepath.Join(cacheRoot, "gum", "default", "notify.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	done := make(chan struct{})
	MaybeNotify(&buf, "v0.1.0", "default", true, stubFetcher{tag: "v0.3.0"}, done)
	<-done

	// Stale cache had latest=v0.1.0 (== current), so no warning this run.
	if buf.Len() != 0 {
		t.Errorf("stale-cache-same-version run should be silent; got %q", buf.String())
	}
	// But the goroutine MUST have refreshed the cache to v0.3.0 for next run.
	refreshed, ok, fresh := readCache(cachePath)
	if !ok || !fresh {
		t.Fatalf("cache was not refreshed (ok=%v fresh=%v)", ok, fresh)
	}
	if refreshed.LatestVersion != "v0.3.0" {
		t.Errorf("cache LatestVersion = %q, want v0.3.0", refreshed.LatestVersion)
	}
}

func TestMaybeNotifyMissingCacheTriggersAsyncRefresh(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	var buf bytes.Buffer
	done := make(chan struct{})
	MaybeNotify(&buf, "v0.1.0", "default", true, stubFetcher{tag: "v0.2.0"}, done)
	<-done

	// No cache existed → no warning this run.
	if buf.Len() != 0 {
		t.Errorf("missing-cache run should be silent; got %q", buf.String())
	}
	cachePath := filepath.Join(cacheRoot, "gum", "default", "notify.json")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache was not created: %v", err)
	}
	entry, ok, fresh := readCache(cachePath)
	if !ok || !fresh || entry.LatestVersion != "v0.2.0" {
		t.Errorf("cache after refresh: ok=%v fresh=%v latest=%q (want v0.2.0)", ok, fresh, entry.LatestVersion)
	}
}

func TestMaybeNotifyFetcherErrorIsSwallowed(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	var buf bytes.Buffer
	done := make(chan struct{})
	MaybeNotify(&buf, "v0.1.0", "default", true, stubFetcher{err: errors.New("offline")}, done)
	<-done

	if buf.Len() != 0 {
		t.Errorf("fetcher error must not surface on stderr; got %q", buf.String())
	}
	// No cache should exist after a failed fetch.
	cachePath := filepath.Join(cacheRoot, "gum", "default", "notify.json")
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Errorf("expected no cache file after fetch error, got err=%v", err)
	}
}

func TestWriteCacheAtomicAndMode0600(t *testing.T) {
	cacheRoot := t.TempDir()
	path := filepath.Join(cacheRoot, "gum", "default", "notify.json")
	entry := cacheEntry{CheckedAt: time.Now().UTC(), LatestVersion: "v9.9.9"}
	if err := writeCache(path, entry); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("cache mode = %o, want 0600", info.Mode().Perm())
	}
}

// TestWriteCacheMkdirAllError drives the parent-create error branch:
// pointing path's parent at an existing regular file forces MkdirAll
// to fail. Without this branch covered, a misconfigured cache root
// would silently lose notifier state instead of surfacing the cause.
func TestWriteCacheMkdirAllError(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "block")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	// path forces MkdirAll to try to create blocker/sub, which fails
	// because blocker is a regular file, not a dir.
	path := filepath.Join(blocker, "sub", "notify.json")
	entry := cacheEntry{CheckedAt: time.Now().UTC(), LatestVersion: "v1"}
	if err := writeCache(path, entry); err == nil {
		t.Fatal("writeCache returned nil; want MkdirAll error")
	}
}

// TestWriteCacheWriteFileError drives the os.WriteFile branch: planting
// a *directory* at path+".tmp" causes WriteFile to fail with EISDIR.
// Without this branch covered, a stale lock-style temp directory would
// silently swallow notifier state.
func TestWriteCacheWriteFileError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notify.json")
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	entry := cacheEntry{CheckedAt: time.Now().UTC(), LatestVersion: "v1"}
	if err := writeCache(path, entry); err == nil {
		t.Fatal("writeCache returned nil; want WriteFile error")
	}
}

// TestWriteCacheRenameError drives the os.Rename branch: planting a
// non-empty directory at path causes Rename of a file onto it to fail.
// The function must surface the error and clean up the .tmp file.
func TestWriteCacheRenameError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notify.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "occupant"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := cacheEntry{CheckedAt: time.Now().UTC(), LatestVersion: "v1"}
	if err := writeCache(path, entry); err == nil {
		t.Fatal("writeCache returned nil; want Rename error onto non-empty dir")
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Errorf("temp file leaked at %s", path+".tmp")
	}
}

// TestHTTPFetcherLatest exercises the GitHub releases fetch wrapper.
//   - 200 with tag_name → returns tag.
//   - Empty tag_name → error (defends against half-populated responses).
//   - Non-200 status → error containing the status code.
//   - Cancelled context → error.
//
// The httptest.Server lets us run these synchronously without touching the
// real api.github.com.
func TestHTTPFetcherLatest(t *testing.T) {
	t.Run("happy_path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/repos/ehmo/gum/releases/latest") {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.2.3"})
		}))
		defer srv.Close()

		f := HTTPFetcher{Client: srv.Client()}
		// Use a custom transport that rewrites api.github.com → srv.URL.
		f.Client = newRewriteClient(srv.URL)

		got, err := f.Latest(context.Background(), "ehmo/gum")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if got != "v1.2.3" {
			t.Errorf("tag = %q, want v1.2.3", got)
		}
	})

	t.Run("empty_tag_name_is_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": ""})
		}))
		defer srv.Close()

		f := HTTPFetcher{Client: newRewriteClient(srv.URL)}
		_, err := f.Latest(context.Background(), "ehmo/gum")
		if err == nil || !strings.Contains(err.Error(), "empty tag_name") {
			t.Errorf("err = %v, want empty tag_name", err)
		}
	})

	t.Run("non_200_returns_status_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"rate limited"}`))
		}))
		defer srv.Close()

		f := HTTPFetcher{Client: newRewriteClient(srv.URL)}
		_, err := f.Latest(context.Background(), "ehmo/gum")
		if err == nil || !strings.Contains(err.Error(), "403") {
			t.Errorf("err = %v, want 403 reference", err)
		}
	})

	t.Run("context_cancelled_returns_error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(50 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1"})
		}))
		defer srv.Close()

		f := HTTPFetcher{Client: newRewriteClient(srv.URL)}
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // pre-cancelled
		_, err := f.Latest(ctx, "ehmo/gum")
		if err == nil {
			t.Errorf("expected cancellation error, got nil")
		}
	})
}

// newRewriteClient returns an http.Client whose transport rewrites any
// request to api.github.com to the given test-server URL.
func newRewriteClient(serverURL string) *http.Client {
	return &http.Client{
		Transport: &rewriteRoundTripper{target: serverURL},
		Timeout:   2 * time.Second,
	}
}

type rewriteRoundTripper struct{ target string }

func (rr *rewriteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "api.github.com" {
		// Replace scheme + host with the test server's.
		parsed, err := http.NewRequest(req.Method, rr.target+req.URL.Path, req.Body)
		if err != nil {
			return nil, err
		}
		parsed = parsed.WithContext(req.Context())
		parsed.Header = req.Header
		return http.DefaultTransport.RoundTrip(parsed)
	}
	return http.DefaultTransport.RoundTrip(req)
}
