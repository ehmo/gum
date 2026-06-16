package lro_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/lro"
)

// TestHTTPFetcherRouteCompute simulates a Compute ZoneOperations GET against
// a fake compute.googleapis.com server. The fetcher must consult the routing
// table, substitute the path, and return the parsed Operation.
func TestHTTPFetcherRouteCompute(t *testing.T) {
	var gotPath string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"projects/p/zones/us/operations/op-1","done":true,"response":{"kind":"compute#operation"}}`))
	}))
	defer srv.Close()

	// Point compute.googleapis.com at the fake server by overriding HTTPClient.
	fetcher := &lro.HTTPFetcher{
		HTTPClient: clientWithRewrite(srv, "compute.googleapis.com"),
	}
	st, err := fetcher.Fetch(context.Background(), "projects/p/zones/us/operations/op-1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !st.Done {
		t.Errorf("Done=false want true")
	}
	want := "/compute/v1/projects/p/zones/us/operations/op-1"
	if gotPath != want {
		t.Errorf("server got path=%q want %q", gotPath, want)
	}
}

// TestHTTPFetcherFallbackOpsTail covers spec §5.7 fallback step 2: when no
// routing-table entry matches, the fetcher tries GET
// {LastHost}/v1/operations/{tail}.
func TestHTTPFetcherFallbackOpsTail(t *testing.T) {
	var gotPath string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"name":"unknownservice/12345","done":false}`))
	}))
	defer srv.Close()

	fetcher := &lro.HTTPFetcher{
		HTTPClient: clientWithRewrite(srv, "lastupstream.example.com"),
		LastHost:   "lastupstream.example.com",
	}
	st, err := fetcher.Fetch(context.Background(), "unknownservice/12345")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if st.Done {
		t.Errorf("Done=true want false")
	}
	if gotPath != "/v1/operations/12345" {
		t.Errorf("fallback-1 got path=%q want /v1/operations/12345", gotPath)
	}
}

// TestHTTPFetcherFallbackFullName covers spec §5.7 fallback step 3: when
// step 2 returns 404, the fetcher tries GET {LastHost}/v1/{operation_name}.
func TestHTTPFetcherFallbackFullName(t *testing.T) {
	var paths []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		// First call (tail attempt) → 404; second call (full name) → 200.
		if strings.HasPrefix(r.URL.Path, "/v1/operations/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"name":"unknownservice/long/path/abc","done":true,"response":{}}`))
	}))
	defer srv.Close()

	fetcher := &lro.HTTPFetcher{
		HTTPClient: clientWithRewrite(srv, "alt.example.com"),
		LastHost:   "alt.example.com",
	}
	st, err := fetcher.Fetch(context.Background(), "unknownservice/long/path/abc")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !st.Done {
		t.Errorf("Done=false want true after fallback-2")
	}
	if len(paths) != 2 {
		t.Fatalf("got %d server hits want 2 (fallback-1 then fallback-2): %v", len(paths), paths)
	}
	if paths[1] != "/v1/unknownservice/long/path/abc" {
		t.Errorf("fallback-2 got path=%q", paths[1])
	}
}

// TestHTTPFetcherReturnsLROUnroutable pins that with no routing-table hit
// and no LastHost, the fetcher surfaces ErrUnroutable verbatim so the
// dispatch layer can map it to the LRO_UNROUTABLE envelope.
func TestHTTPFetcherReturnsLROUnroutable(t *testing.T) {
	fetcher := &lro.HTTPFetcher{}
	_, err := fetcher.Fetch(context.Background(), "unknownservice/foo")
	if !errors.Is(err, lro.ErrUnroutable) {
		t.Errorf("got %v want ErrUnroutable", err)
	}
}

// TestHTTPFetcherAuthInjectInvoked pins that the AuthInject callback fires
// before the request is sent so quota project + bearer token can attach.
func TestHTTPFetcherAuthInjectInvoked(t *testing.T) {
	var sawAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"name":"projects/p/zones/us/operations/op","done":true}`))
	}))
	defer srv.Close()

	fetcher := &lro.HTTPFetcher{
		HTTPClient: clientWithRewrite(srv, "compute.googleapis.com"),
		AuthInject: func(req *http.Request) error {
			req.Header.Set("Authorization", "Bearer test-token")
			return nil
		},
	}
	_, err := fetcher.Fetch(context.Background(), "projects/p/zones/us/operations/op")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if sawAuth != "Bearer test-token" {
		t.Errorf("Authorization header=%q want Bearer test-token", sawAuth)
	}
}

// clientWithRewrite returns an http.Client whose Transport rewrites every
// outbound request URL to point at srv, regardless of the original host.
// Lets tests pretend they're talking to compute.googleapis.com.
func clientWithRewrite(srv *httptest.Server, expectedHost string) *http.Client {
	srvURL, _ := url.Parse(srv.URL)
	return &http.Client{
		Transport: &rewriteTransport{
			expectedHost: expectedHost,
			target:       srvURL,
			inner:        srv.Client().Transport,
		},
	}
}

type rewriteTransport struct {
	expectedHost string
	target       *url.URL
	inner        http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != rt.expectedHost {
		// surface the mismatch so the test fails clearly instead of getting
		// silently routed to the loopback server with the wrong assumption.
		return nil, errBadHost{want: rt.expectedHost, got: r.URL.Host}
	}
	r.URL.Scheme = rt.target.Scheme
	r.URL.Host = rt.target.Host
	r.Host = rt.target.Host
	return rt.inner.RoundTrip(r)
}

type errBadHost struct{ want, got string }

func (e errBadHost) Error() string {
	return "rewrite: expected host " + e.want + " but got " + e.got
}

// TestOperationDocPartialParse pins that an Operation response missing the
// optional response/error/metadata fields still surfaces name+done cleanly.
func TestOperationDocPartialParse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"operations/x"}`))
	}))
	defer srv.Close()

	fetcher := &lro.HTTPFetcher{
		HTTPClient: clientWithRewrite(srv, "googleapis.com"),
		LastHost:   "googleapis.com",
	}
	st, err := fetcher.Fetch(context.Background(), "unmatched/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	m, ok := st.Result.(map[string]any)
	if !ok {
		t.Fatalf("Result not map, got %T", st.Result)
	}
	if m["done"] != false {
		t.Errorf("done=%v want false", m["done"])
	}
	if m["name"] != "operations/x" {
		t.Errorf("name=%v want operations/x", m["name"])
	}
}

// TestNotOperationShapeFallthrough pins that a 200 OK with non-Operation
// JSON body advances to the next fallback rather than reporting Done.
func TestNotOperationShapeFallthrough(t *testing.T) {
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			// First call returns non-Operation JSON (e.g., a directory listing).
			_, _ = w.Write([]byte(`{"items":[{"id":"a"}]}`))
		default:
			// Second call returns a valid Operation.
			_, _ = w.Write([]byte(`{"name":"unmatched/x","done":true,"response":{}}`))
		}
	}))
	defer srv.Close()

	fetcher := &lro.HTTPFetcher{
		HTTPClient: clientWithRewrite(srv, "ex.example.com"),
		LastHost:   "ex.example.com",
	}
	st, err := fetcher.Fetch(context.Background(), "unmatched/x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !st.Done {
		t.Errorf("after fallthrough, Done=false want true")
	}
	if calls != 2 {
		t.Errorf("server saw %d calls want 2", calls)
	}
}

