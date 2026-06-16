package risor_test

import (
	"context"
	"net"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/sandbox/risor"
)

// rawCloseListener starts a TCP listener whose accept loop immediately
// closes every connection without speaking HTTP/TLS. It returns the
// "host:port" to target plus a stop func that closes the listener AND
// blocks until the accept goroutine has actually exited — the caller
// must invoke stop before goleak.VerifyNone (via defer ordering) so no
// stray goroutine trips the leak check.
func rawCloseListener(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			_ = c.Close() // hang up immediately
		}
	}()
	stop := func() {
		_ = ln.Close()
		<-done // wait for the accept goroutine to return
	}
	return ln.Addr().String(), stop
}

// TestRoundTripReadResponseErrorWraps pins egress.go:95-98 (the
// http.ReadResponse failure arm) together with egress.go:81-86's
// ctxErrOrWrap fall-through (85.3,85.39 — the non-ctx-done branch that
// wraps the underlying error). The server accepts the connection and
// hangs up before sending any response, so the request Write succeeds
// but ReadResponse hits EOF while the context is still live — exactly
// the path that must surface a wrapped "read response" error rather
// than a context error.
func TestRoundTripReadResponseErrorWraps(t *testing.T) {
	defer goleak.VerifyNone(t)

	addr, stop := rawCloseListener(t)
	defer stop()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host: %v", err)
	}
	opts := risor.Options{
		AllowInsecureHTTP:  true,
		AllowedHosts:       []string{host},
		AllowPrivateEgress: true, // loopback listener; reach the transport arms past the SSRF guard
	}
	// http:// so dial+write succeed; the immediate hang-up forces the
	// read-response error arm with a live (non-cancelled) context.
	script := `gum_http_get("http://` + addr + `/")`
	_, err = risor.Run(context.Background(), script, opts)
	if err == nil {
		t.Fatal("expected error from RoundTrip read-response failure, got nil")
	}
}

// TestRoundTripTLSHandshakeErrorWraps pins egress.go:59-62 — the TLS
// HandshakeContext failure arm. The server accepts the TCP connection
// then closes it without performing a TLS handshake, so an https://
// request dials successfully but the handshake fails, exercising the
// rawConn.Close()+"tls handshake" wrap path.
func TestRoundTripTLSHandshakeErrorWraps(t *testing.T) {
	defer goleak.VerifyNone(t)

	addr, stop := rawCloseListener(t)
	defer stop()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host: %v", err)
	}
	opts := risor.Options{
		AllowedHosts:       []string{host},
		AllowPrivateEgress: true, // loopback listener; reach the TLS handshake arm past the SSRF guard
	}
	// https:// against a non-TLS server → dial OK, handshake fails.
	script := `gum_http_get("https://` + addr + `/")`
	_, err = risor.Run(context.Background(), script, opts)
	if err == nil {
		t.Fatal("expected error from RoundTrip TLS handshake failure, got nil")
	}
	// Sanity: the failure should not be an allowlist denial (host is
	// allowed); it must come from the transport layer.
	if strings.Contains(err.Error(), "EGRESS_HOST_DENIED") {
		t.Errorf("got allowlist denial, want transport-layer failure: %v", err)
	}
}
