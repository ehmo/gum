package lro_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ehmo/gum/internal/lro"
)

// TestHTTPFetcherGRPCFallbackSucceeds pins Fetch's
// `routing match is gRPC → tryREST(googleapis.com, /v1/{op}) succeeds`
// arm (httpfetcher.go:47-53). v0.1.0 has no gRPC client wired, so when
// routing.Lookup returns TransportGRPC the fetcher MUST attempt a REST
// fallback against googleapis.com before giving up. Operation names
// starting with "operations/" trigger the gRPC route per
// routing.ServiceByPrefix.
func TestHTTPFetcherGRPCFallbackSucceeds(t *testing.T) {
	var gotPath string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"name":"operations/abc","done":true,"response":{}}`))
	}))
	defer srv.Close()

	fetcher := &lro.HTTPFetcher{
		HTTPClient: clientWithRewrite(srv, "googleapis.com"),
	}
	st, err := fetcher.Fetch(context.Background(), "operations/abc")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !st.Done {
		t.Errorf("Done=false; want true (server returned done=true)")
	}
	if gotPath != "/v1/operations/abc" {
		t.Errorf("server saw path=%q; want /v1/operations/abc", gotPath)
	}
}

// TestHTTPFetcherGRPCFallbackFailsReturnsUnroutable pins Fetch's
// `routing match is gRPC → tryREST fails → return ErrUnroutable` arm
// (httpfetcher.go:54). When the REST fallback against googleapis.com
// also fails (e.g. 404), the fetcher returns ErrUnroutable rather than
// silently swallowing the gRPC route. The caller (poller) translates
// that into LRO_UNROUTABLE per spec §5.7.
func TestHTTPFetcherGRPCFallbackFailsReturnsUnroutable(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	fetcher := &lro.HTTPFetcher{
		HTTPClient: clientWithRewrite(srv, "googleapis.com"),
	}
	_, err := fetcher.Fetch(context.Background(), "operations/abc")
	if !errors.Is(err, lro.ErrUnroutable) {
		t.Errorf("Fetch err=%v; want ErrUnroutable", err)
	}
}
