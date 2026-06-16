package notify_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/notify"
)

// TestCachePathHomeUnavailableSurfacesError pins the
// `UserHomeDir err → return "", err` arm of CachePath (notify.go:61-63).
// Reached when XDG_CACHE_HOME is unset AND HOME is unset (CI sandboxes,
// k8s pods). CachePath surfaces UserHomeDir's err verbatim — the
// caller (MaybeNotify) silently bails so notifier failures never leak.
//
// Lifts CachePath 88.9 → 100. Windows skip because the HOME-unset
// trick is darwin/linux-specific.
func TestCachePathHomeUnavailableSurfacesError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-unset trick is darwin/linux-specific")
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")

	got, err := notify.CachePath("alpha")
	if err == nil {
		t.Fatalf("CachePath(HOME=unset)=%q nil err; want UserHomeDir surface", got)
	}
	if got != "" {
		t.Errorf("got=%q; want \"\" on err (don't leak partial path)", got)
	}
}

// TestMaybeNotifyCachePathErrorBailsSilently pins MaybeNotify's
// `CachePath err → return` arm (notify.go:218-221). Reached via the
// same HOME-unset trick. MaybeNotify MUST swallow the err with no
// stderr output AND close the done chan so test callers don't hang.
// This is the silent-on-config-failure guarantee documented in the
// MaybeNotify contract — notifier infrastructure errors NEVER surface
// to the user.
func TestMaybeNotifyCachePathErrorBailsSilently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME-unset trick is darwin/linux-specific")
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")

	var stderr bytes.Buffer
	done := make(chan struct{})
	notify.MaybeNotify(&stderr, "v1.0.0", "alpha", true, fakeFetcher{tag: "v9.9.9"}, done)
	<-done

	if got := stderr.String(); got != "" {
		t.Errorf("stderr=%q; want empty (CachePath err must be silent)", got)
	}
}

type fakeFetcher struct {
	tag string
	err error
}

func (f fakeFetcher) Latest(ctx context.Context, repo string) (string, error) {
	return f.tag, f.err
}

// TestReadCacheCorruptJSONIsTreatedAsCacheMiss pins readCache's
// `json.Unmarshal err → return zero, false, false` arm
// (notify.go:76-78). Reached when notify.json exists but is corrupt
// (truncated write, manual edit). readCache treats it as a miss so
// MaybeNotify silently triggers a fresh refresh; an err return here
// would propagate up and break the "notifier never errors" contract.
//
// Indirectly verified through MaybeNotify: corrupt cache + fetcher
// returning newer version triggers async refresh that writes a clean
// cache, demonstrating no warning was emitted from the stale-corrupt
// entry (since LatestVersion fails to decode).
func TestReadCacheCorruptJSONIsTreatedAsCacheMiss(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)

	// Plant corrupt cache at the path CachePath would resolve.
	path, err := notify.CachePath("alpha")
	if err != nil {
		t.Fatalf("CachePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("plant corrupt cache: %v", err)
	}

	var stderr bytes.Buffer
	done := make(chan struct{})
	notify.MaybeNotify(&stderr, "v1.0.0", "alpha", true, fakeFetcher{tag: "v9.9.9"}, done)
	<-done

	// No warning emitted (corrupt entry treated as miss, async refresh
	// silently writes a fresh cache).
	if got := stderr.String(); got != "" {
		t.Errorf("stderr=%q; want empty (corrupt cache must NOT emit warning)", got)
	}
	// Cache is now well-formed (async refresh wrote it).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read refreshed cache: %v", err)
	}
	if !strings.Contains(string(data), "v9.9.9") {
		t.Errorf("refreshed cache=%q; want fakeFetcher tag v9.9.9", data)
	}
}

// TestHTTPFetcherLatestDefaultClientUsedWhenNil pins HTTPFetcher's
// `client == nil → &http.Client{Timeout: CheckTimeout}` arm
// (notify.go:120-122). Verifies the zero-value HTTPFetcher{} works
// out of the box — important because MaybeNotify's
// `fetcher == nil → fetcher = HTTPFetcher{}` fallback relies on this.
//
// Uses an httptest server in place of api.github.com via a custom
// transport-rewriting client wouldn't help (the default client path
// can't be rewired). Instead we override the http.DefaultTransport
// for the lifetime of the test? No — simpler: we can't intercept the
// default client, but we CAN observe the err arm of the default
// client by pointing the fetcher at a URL that resolves to nowhere.
// Use a context with immediate cancellation: client.Do errs with
// context.Canceled → covers the default-client branch without ever
// touching the real network.
func TestHTTPFetcherLatestDefaultClientUsedWhenNil(t *testing.T) {
	f := notify.HTTPFetcher{Client: nil}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so client.Do fails immediately

	_, err := f.Latest(ctx, "ehmo/gum")
	if err == nil {
		t.Fatal("Latest(pre-cancelled ctx)=nil err; want context.Canceled via default client")
	}
}

// TestHTTPFetcherLatestDecodeErrorSurfaces pins
// `json.NewDecoder.Decode err` arm (notify.go:143-145) of
// HTTPFetcher.Latest. Reached when the server returns 200 with
// non-JSON body (proxy mangling, intermediary inserting HTML).
// Without the err surface, callers would silently cache an empty
// tag_name which downstream MaybeNotify already short-circuits — but
// the decode err itself is the operator's signal that the API surface
// changed.
//
// Uses a custom http.Client with a Transport that routes
// "api.github.com" requests to our httptest server, leaving the
// Latest URL hardcoded as-is.
func TestHTTPFetcherLatestDecodeErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json-at-all"))
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: rewriteTo{target: srv.URL}}
	f := notify.HTTPFetcher{Client: client}

	_, err := f.Latest(context.Background(), "ehmo/gum")
	if err == nil {
		t.Fatal("Latest(garbage body)=nil err; want json.Decode err")
	}
}

// rewriteTo is a RoundTripper that rewrites any request URL to a fixed
// target host:port, preserving the path/method/body. Used to redirect
// api.github.com calls to httptest in tests without modifying the
// fetcher's hardcoded URL builder.
type rewriteTo struct {
	target string
}

func (r rewriteTo) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite scheme + host to the target; leave path/query.
	u := *req.URL
	// Parse target into scheme+host without importing net/url at module level.
	rest := strings.TrimPrefix(r.target, "http://")
	rest = strings.TrimPrefix(rest, "https://")
	u.Scheme = "http"
	u.Host = rest
	req2 := req.Clone(req.Context())
	req2.URL = &u
	return http.DefaultTransport.RoundTrip(req2)
}

// TestSplitSemverNonNumericSegmentReturnsNil pins splitSemver's
// `strconv.Atoi err → return nil` arm (notify.go:188-190). Reached
// when one of the three semver positions is non-numeric (e.g., a
// tag-name typo "v1.x.0"). Indirectly verified through
// CompareVersions: nil splitSemver result on either side → return 0
// (treated as equal, no warning) — the silent-on-malformed-tag
// guarantee that prevents notifier noise on weird upstream releases.
func TestSplitSemverNonNumericSegmentReturnsNil(t *testing.T) {
	// Both inputs malformed → both splitSemver return nil → equal (0).
	if got := notify.CompareVersions("v1.x.0", "v2.0.0"); got != 0 {
		t.Errorf("CompareVersions(v1.x.0, v2.0.0)=%d; want 0 (malformed → equal)", got)
	}
}
