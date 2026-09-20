package risor

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// holdOpenListener accepts connections and keeps them open until stop runs, so
// a test can drive a write failure that comes from the request body rather than
// from the peer hanging up. stop closes the listener, waits for the accept
// goroutine, then closes every accepted connection.
func holdOpenListener(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	stop := func() {
		_ = ln.Close()
		<-done
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
	}
	return ln.Addr().String(), stop
}

// errReader fails on the first Read so a request body write cannot succeed.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("body read blew up") }

// TestRoundTripHTTPSDefaultsToPort443 pins the scheme-derived default port. A
// URL with no port must dial :443 for https, the same way the existing http
// case dials :80. The SSRF guard reports the address it refused, which is what
// makes the derived port observable without opening a socket.
func TestRoundTripHTTPSDefaultsToPort443(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://127.0.0.1/x", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = syncDialTransport{}.RoundTrip(req)
	if err == nil {
		t.Fatal("RoundTrip err = nil; want the private-IP denial")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:443") {
		t.Errorf("RoundTrip err = %v; want the refused address to carry the default https port 443", err)
	}
}

// TestRoundTripSurfacesAWriteFailure pins the req.Write failure arm. The dial
// and the connection both succeed, so the only thing that can fail is the body
// copy, and the error has to come back wrapped as "write request" rather than
// as a nil response.
func TestRoundTripSurfacesAWriteFailure(t *testing.T) {
	addr, stop := holdOpenListener(t)
	defer stop()

	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/", errReader{})
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := syncDialTransport{allowPrivateEgress: true}.RoundTrip(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("RoundTrip err = nil; want the request-write failure")
	}
	if !strings.Contains(err.Error(), "write request") {
		t.Errorf("RoundTrip err = %v; want it wrapped as \"write request\"", err)
	}
}

// TestConnClosingBodyReportsTheConnCloseFailure pins the errors.Join arm of
// connClosingBody.Close. A second close of a TCP connection fails, and that
// failure must reach the caller instead of being swallowed, because the caller
// uses it to tell a clean hang-up from a leaked socket.
func TestConnClosingBodyReportsTheConnCloseFailure(t *testing.T) {
	addr, stop := holdOpenListener(t)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	body := &connClosingBody{
		ReadCloser: io.NopCloser(strings.NewReader("")),
		conn:       conn,
		stop:       func() bool { return true },
	}
	if err := body.Close(); err == nil {
		t.Error("Close err = nil; want the already-closed connection reported")
	}
}

// TestEgressDialControlShapes drives the SSRF guard directly. The runtime hands
// it "ip:port", but the guard must stay active for a bare address too: a silent
// pass there would turn a malformed address into an open door.
func TestEgressDialControlShapes(t *testing.T) {
	cases := []struct {
		name    string
		address string
		blocked bool
	}{
		{"loopback_with_port", "127.0.0.1:443", true},
		{"rfc1918_with_port", "10.1.2.3:80", true},
		{"link_local_metadata", "169.254.169.254:80", true},
		{"unspecified", "0.0.0.0:80", true},
		{"ula_v6", "[fd00::1]:443", true},
		{"public_with_port", "8.8.8.8:443", false},
		{"bare_loopback_no_port", "127.0.0.1", true},
		{"bare_public_no_port", "203.0.113.5", false},
		{"not_an_ip", "example.com:443", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := egressDialControl("tcp", tc.address, nil)
			if tc.blocked && err == nil {
				t.Fatalf("egressDialControl(%q) = nil; want a denial", tc.address)
			}
			if !tc.blocked && err != nil {
				t.Fatalf("egressDialControl(%q) = %v; want nil", tc.address, err)
			}
			if tc.blocked && !strings.Contains(err.Error(), "EGRESS_PRIVATE_IP_DENIED") {
				t.Errorf("egressDialControl(%q) = %v; want EGRESS_PRIVATE_IP_DENIED", tc.address, err)
			}
		})
	}
}
