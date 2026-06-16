package lro

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// rewriteForHost returns an http.Client that rewrites any host to the
// httptest server URL. tryREST builds "https://host+path"; the test
// harness must route that to the loopback server regardless of the
// fake host we hand it.
func rewriteForHost(srv *httptest.Server) *http.Client {
	srvURL, _ := url.Parse(srv.URL)
	return &http.Client{Transport: &testRewrite{target: srvURL, inner: srv.Client().Transport}}
}

type testRewrite struct {
	target *url.URL
	inner  http.RoundTripper
}

func (rt *testRewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r.URL.Scheme = rt.target.Scheme
	r.URL.Host = rt.target.Host
	r.Host = rt.target.Host
	return rt.inner.RoundTrip(r)
}

// TestTryRESTAuthInjectFailureSurfacesError pins the
// `AuthInject err → return nil, err` arm. AuthInject is the callback
// that attaches Authorization headers; if credential acquisition
// fails (token expired mid-fetch, ADC bag broken, etc.) tryREST MUST
// surface that error directly to the caller rather than firing the
// HTTP GET with no auth — which would 401, get rewritten by the
// truncate path, and obscure the real auth failure.
func TestTryRESTAuthInjectFailureSurfacesError(t *testing.T) {
	authErr := errors.New("token expired")
	f := &HTTPFetcher{
		HTTPClient: http.DefaultClient,
		AuthInject: func(req *http.Request) error { return authErr },
	}
	_, err := f.tryREST(context.Background(), "googleapis.com", "/v1/operations/op-1")
	if err == nil {
		t.Fatal("tryREST(authInject=err)=nil err; want surface")
	}
	if !errors.Is(err, authErr) {
		t.Errorf("err=%v; want wrap of %v", err, authErr)
	}
}

// TestTryRESTHTTPDoFailureSurfacesError pins the
// `HTTPClient.Do err → return nil, err` arm. Operation polls happen
// over the open internet; any transport-level failure (DNS, TLS
// handshake, mid-request reset) MUST surface so the dispatch poller
// can back off rather than treating the failure as "not an
// operation" (the ErrUnroutable wire signal would silently retry
// the wrong fallback).
func TestTryRESTHTTPDoFailureSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	client := rewriteForHost(srv)
	srv.Close() // ECONNREFUSED on next request

	f := &HTTPFetcher{HTTPClient: client}
	_, err := f.tryREST(context.Background(), "googleapis.com", "/v1/operations/op-1")
	if err == nil {
		t.Fatal("tryREST(server closed)=nil err; want transport err surface")
	}
	if errors.Is(err, ErrUnroutable) {
		t.Errorf("err=%v; want non-ErrUnroutable so poller can back off", err)
	}
}

// TestTryRESTNon404HTTPErrorSurfacesWrap pins the
// `resp.StatusCode >= 400 (non-404) → return wrapped err` arm.
// 404 is the "not an operation" signal that advances the fetcher to
// the next fallback template; ANY other 4xx/5xx MUST surface as a
// wrapped error so the caller doesn't silently round-trip another
// fallback against a server that's already saying "auth required" or
// "internal error". The wrap MUST include the status code so
// operators can grep audit logs.
func TestTryRESTNon404HTTPErrorSurfacesWrap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"upstream failure"}`))
	}))
	defer srv.Close()
	f := &HTTPFetcher{HTTPClient: rewriteForHost(srv)}
	_, err := f.tryREST(context.Background(), "googleapis.com", "/v1/operations/op-1")
	if err == nil {
		t.Fatal("tryREST(500)=nil err; want wrap")
	}
	if errors.Is(err, ErrUnroutable) {
		t.Errorf("err=%v; want NOT ErrUnroutable for 5xx (that signal is reserved for 404)", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err=%q; want status code in wrap", err)
	}
}

// TestTryRESTMalformedJSONReturnsUnroutable pins the
// `json.Unmarshal err → return nil, ErrUnroutable` arm. A 200 body
// that doesn't parse as an Operation is treated the same as a 404:
// "not an operation at this URL" — advance to next template. Without
// this arm a garbled body would surface as a generic parse error,
// stopping the fallback chain even though attempt 3 might succeed.
func TestTryRESTMalformedJSONReturnsUnroutable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not { json ] at all"))
	}))
	defer srv.Close()
	f := &HTTPFetcher{HTTPClient: rewriteForHost(srv)}
	_, err := f.tryREST(context.Background(), "googleapis.com", "/v1/operations/op-1")
	if !errors.Is(err, ErrUnroutable) {
		t.Errorf("err=%v; want ErrUnroutable so caller advances to next fallback", err)
	}
}
