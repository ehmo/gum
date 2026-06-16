package httputil_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/httputil"
)

// TestLimitedTransportCapsBody pins the OOM guard: a body larger than the cap
// fails the read instead of buffering unboundedly.
func TestLimitedTransportCapsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1000)))
	}))
	defer srv.Close()

	c := &http.Client{Transport: &httputil.LimitedTransport{MaxBytes: 100}}
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Fatal("reading a 1000-byte body through a 100-byte cap should error")
	}
}

// TestCappedClientUnderCapOK confirms a normal (under-cap) response reads fine
// and the body is intact.
func TestCappedClientUnderCapOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("small body"))
	}))
	defer srv.Close()

	resp, err := httputil.CappedClient(nil).Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil || string(b) != "small body" {
		t.Fatalf("under-cap read: b=%q err=%v", b, err)
	}
}
