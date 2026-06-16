// Package risor — egress allowlist helper for gum-9wb (spec §11).
package risor

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
)

// matchHostAllowlist is documented in sandbox.go.
// (function body is in sandbox.go)

// isBlockedEgressIP reports whether ip falls in a range the sandbox must not
// reach when AllowPrivateEgress is false (gum-j1ly). It blocks the SSRF-classic
// internal ranges — loopback, RFC1918 / RFC4193-ULA private space, link-local
// (which covers the 169.254.169.254 cloud-metadata endpoint), and the
// unspecified address. The check runs against the *resolved* dial address, so
// it catches both literal-IP allowlist entries and hostnames that resolve into
// an internal range (DNS-rebinding), which a hostname-string allowlist alone
// cannot. IPv4-mapped IPv6 forms are normalized by the net.IP predicates.
func isBlockedEgressIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// syncDialTransport is a minimal http.RoundTripper that dials synchronously
// within the request's context. It never spawns background goroutines, unlike
// http.Transport which deliberately detaches its dial goroutines from the
// request context (via context.WithoutCancel in getConn) to support connection
// reuse. For the sandbox's short-lived, no-keepalive HTTP calls this
// transparent detachment causes goroutine leaks that goleak catches in tests.
//
// Implementation: resolve → dial → (TLS) → write request → read response,
// all inside the request context. One goroutine, no background work.
type syncDialTransport struct {
	// allowPrivateEgress, when true, disables the SSRF guard so the dialer may
	// reach private/loopback/link-local addresses (test-only). See Options.
	allowPrivateEgress bool
}

func (t syncDialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	addr := req.URL.Host
	if req.URL.Port() == "" {
		if strings.ToLower(req.URL.Scheme) == "https" {
			addr = net.JoinHostPort(req.URL.Hostname(), "443")
		} else {
			addr = net.JoinHostPort(req.URL.Hostname(), "80")
		}
	}

	// Dial synchronously with the request context (includes the HTTPTimeout).
	// FallbackDelay: -1 disables Happy Eyeballs to avoid parallel goroutines.
	dialer := &net.Dialer{FallbackDelay: -1}
	if !t.allowPrivateEgress {
		// SSRF guard (gum-j1ly): Control runs after DNS resolution with the
		// concrete ip:port about to be dialed, so it rejects both literal
		// private-IP targets and hostnames that resolve into an internal range.
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, splitErr := net.SplitHostPort(address)
			if splitErr != nil {
				host = address
			}
			if isBlockedEgressIP(net.ParseIP(host)) {
				return fmt.Errorf("EGRESS_PRIVATE_IP_DENIED: refusing to dial private/loopback/link-local address %s", address)
			}
			return nil
		}
	}
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	var conn interface {
		net.Conn
	} = rawConn

	// Wrap in TLS if needed.
	if strings.ToLower(req.URL.Scheme) == "https" {
		tlsConn := tls.Client(rawConn, &tls.Config{
			ServerName: req.URL.Hostname(),
			MinVersion: tls.VersionTLS12,
		})
		// Handshake with the request context.
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = rawConn.Close()
			return nil, fmt.Errorf("tls handshake: %w", err)
		}
		conn = tlsConn
	}

	// Propagate context cancellation to the connection I/O.
	// When ctx is done, close the connection so pending reads/writes unblock.
	// Ownership of the cancel handle transfers to connClosingBody on the
	// success path; on error paths the deferred cleanup fires the cancel.
	stopFn := context.AfterFunc(ctx, func() { _ = conn.Close() })
	ownedByBody := false
	defer func() {
		if !ownedByBody {
			stopFn()
		}
	}()

	// ctxErrOrWrap returns the context error if the context is done (so the
	// caller gets "deadline exceeded" / "context canceled" instead of the
	// lower-level "use of closed network connection").
	ctxErrOrWrap := func(op string, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("%s: %w", op, err)
	}

	// Write request.
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, ctxErrOrWrap("write request", err)
	}

	// Read response.
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		_ = conn.Close()
		return nil, ctxErrOrWrap("read response", err)
	}

	// The response body reads from conn. Transfer ownership to connClosingBody.
	resp.Body = &connClosingBody{ReadCloser: resp.Body, conn: conn, stop: stopFn}
	ownedByBody = true
	return resp, nil
}

// connClosingBody wraps a response body and closes the underlying connection
// when the body is closed.
type connClosingBody struct {
	io.ReadCloser
	conn net.Conn
	stop func() bool // cancel the AfterFunc
}

func (b *connClosingBody) Close() error {
	b.stop()
	err := b.ReadCloser.Close()
	if connErr := b.conn.Close(); connErr != nil {
		return errors.Join(err, connErr)
	}
	return err
}
