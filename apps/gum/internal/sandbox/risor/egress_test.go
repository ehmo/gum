// Package risor_test — Red-team tests for gum-9wb: egress allowlist enforcement.
//
// These tests assert that the Risor sandbox properly enforces host-based egress
// controls via Options.AllowedHosts and rejects forbidden schemes before any
// allowlist check. They are written against the GREEN team's target API and are
// expected to FAIL against the current sandbox.go (which has no allowlist).
package risor_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/sandbox/risor"
)

// TestRisorBlocksDefaultNetAndOSGlobals is a regression pin: the Risor v2 default
// module surface must NOT expose http, net, os, fetch, or socket globals. Each
// script must return a non-nil error (compile or runtime). This should already
// pass against the current sandbox.go since Risor v2 restricts these by default;
// if any sub-case passes, that is a real finding.
func TestRisorBlocksDefaultNetAndOSGlobals(t *testing.T) {
	defer goleak.VerifyNone(t)

	scripts := []string{
		`http.get("https://example.com")`,
		`net.dial("tcp", "example.com:80")`,
		`os.exec("ls")`,
		`os.read_file("/etc/passwd")`,
		`fetch("https://example.com")`,
		`socket.connect("example.com:80")`,
	}

	for _, script := range scripts {
		script := script
		t.Run(script, func(t *testing.T) {
			defer goleak.VerifyNone(t)

			opts := risor.Options{} // no Globals
			_, err := risor.Run(context.Background(), script, opts)
			if err == nil {
				t.Errorf("expected error for script %q but got nil — Risor surface may have widened", script)
			}
		})
	}
}

// TestGumHTTPGetRejectsNonAllowlistedHost asserts that gum_http_get with default
// options (allowlist = *.googleapis.com only) rejects hosts outside the allowlist
// with an error containing "EGRESS_HOST_DENIED".
func TestGumHTTPGetRejectsNonAllowlistedHost(t *testing.T) {
	defer goleak.VerifyNone(t)

	opts := risor.Options{} // default allowlist

	_, err := risor.Run(context.Background(), `gum_http_get("https://evil.example.com/foo")`, opts)
	if err == nil {
		t.Fatal("expected error for non-allowlisted host, got nil")
	}
	if !strings.Contains(err.Error(), "EGRESS_HOST_DENIED") {
		t.Errorf("error %q should contain 'EGRESS_HOST_DENIED'", err.Error())
	}
}

// TestGumHTTPGetAllowlistedHostPassesHostGate asserts that a host in the
// allowlist passes the host gate before the SSRF guard runs. It uses a literal
// loopback address so the test never depends on public DNS.
func TestGumHTTPGetAllowlistedHostPassesHostGate(t *testing.T) {
	defer goleak.VerifyNone(t)

	opts := risor.Options{
		AllowInsecureHTTP: true,
		AllowedHosts:      []string{"127.0.0.1"},
		HTTPTimeout:       100 * time.Millisecond,
	}

	_, err := risor.Run(context.Background(), `gum_http_get("http://127.0.0.1/")`, opts)
	if err == nil {
		t.Fatal("expected private-IP denial, got nil")
	}
	if err != nil && strings.Contains(err.Error(), "EGRESS_HOST_DENIED") {
		t.Errorf("error %q should NOT contain 'EGRESS_HOST_DENIED'; allowlisted host should pass the host gate", err.Error())
	}
	if !strings.Contains(err.Error(), "EGRESS_PRIVATE_IP_DENIED") {
		t.Errorf("error %q should contain 'EGRESS_PRIVATE_IP_DENIED'", err.Error())
	}
}

// TestGumHTTPGetHonorsAllowedHostsOption asserts that AllowedHosts extends the
// default allowlist so that a plugin-declared host is allowed through.
func TestGumHTTPGetHonorsAllowedHostsOption(t *testing.T) {
	// Use http:// with AllowInsecureHTTP to avoid TLS certificate validation issues.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer func() {
		srv.Close()
		goleak.VerifyNone(t)
	}()

	// Extract just the hostname (e.g. "127.0.0.1") from the server URL.
	// The allowlist matches on the host component of the URL, not host:port.
	// srv.URL is "http://127.0.0.1:PORT"; trim the scheme then strip ":PORT".
	srvURL := srv.URL
	hostPort := strings.TrimPrefix(srvURL, "http://")
	hostname := hostPort
	if idx := strings.LastIndex(hostPort, ":"); idx >= 0 {
		hostname = hostPort[:idx]
	}

	opts := risor.Options{
		AllowInsecureHTTP:  true,
		AllowedHosts:       []string{hostname},
		AllowPrivateEgress: true, // reach the loopback test server past the SSRF guard
	}

	script := `gum_http_get("` + srvURL + `/")`
	out, err := risor.Run(context.Background(), script, opts)
	if err != nil {
		t.Fatalf("expected nil error for allowlisted host, got: %v", err)
	}

	// Verify that the response map contains status_code 200.
	if out == nil {
		t.Fatal("expected non-nil Output")
	}
	m, ok := out.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected Value to be map[string]any, got %T: %v", out.Value, out.Value)
	}
	statusCode, _ := m["status_code"].(int)
	if statusCode != 200 {
		t.Errorf("expected status_code 200, got %v", m["status_code"])
	}
}

// loopbackHostname extracts the bare host (e.g. "127.0.0.1") from an httptest
// server URL of the form "http://127.0.0.1:PORT".
func loopbackHostname(srvURL string) string {
	hostPort := strings.TrimPrefix(srvURL, "http://")
	if idx := strings.LastIndex(hostPort, ":"); idx >= 0 {
		return hostPort[:idx]
	}
	return hostPort
}

func TestGumHTTPGetRejectsRedirectToNonAllowlistedHost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"should_not":"reach"}`))
	}))

	start := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusFound)
	}))
	defer func() {
		start.Close()
		target.Close()
		goleak.VerifyNone(t)
	}()

	startURL, err := url.Parse(start.URL)
	if err != nil {
		t.Fatalf("parse start URL: %v", err)
	}
	startURL.Host = strings.Replace(startURL.Host, "127.0.0.1", "localhost", 1)

	opts := risor.Options{
		AllowInsecureHTTP:  true,
		AllowedHosts:       []string{"localhost"},
		AllowPrivateEgress: true,
	}

	_, err = risor.Run(context.Background(), `gum_http_get("`+startURL.String()+`")`, opts)
	if err == nil {
		t.Fatal("expected redirect to non-allowlisted host to fail")
	}
	if !strings.Contains(err.Error(), "EGRESS_HOST_DENIED") || !strings.Contains(err.Error(), "redirect host") {
		t.Errorf("err=%v; want redirect EGRESS_HOST_DENIED", err)
	}
}

// TestGumHTTPGetBlocksPrivateIPEgress is the SSRF guard (gum-j1ly): even when a
// host is explicitly in the allowlist, a request that resolves to a private,
// loopback, or link-local address is refused unless AllowPrivateEgress is set.
// A trusted plugin declaring a broad AllowedHosts must not be able to pivot the
// sandbox into the host's internal network (e.g. 169.254.169.254 metadata).
// Here the target is the loopback httptest server, which is in AllowedHosts but
// NOT opted into private egress, so the dial must be denied.
func TestGumHTTPGetBlocksPrivateIPEgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer func() {
		srv.Close()
		goleak.VerifyNone(t)
	}()

	opts := risor.Options{
		AllowInsecureHTTP: true,
		AllowedHosts:      []string{loopbackHostname(srv.URL)},
		// AllowPrivateEgress intentionally unset → loopback must be denied.
	}

	_, err := risor.Run(context.Background(), `gum_http_get("`+srv.URL+`/")`, opts)
	if err == nil {
		t.Fatal("expected egress denial for loopback target, got nil")
	}
	if !strings.Contains(err.Error(), "EGRESS_PRIVATE_IP_DENIED") {
		t.Errorf("error %q should contain 'EGRESS_PRIVATE_IP_DENIED'", err.Error())
	}
}

// TestGumHTTPGetAllowsPrivateIPWhenOptedIn is the escape-hatch control: with
// AllowPrivateEgress set, the same loopback request that the SSRF guard blocks
// is permitted (this is the path that lets tests reach httptest servers).
func TestGumHTTPGetAllowsPrivateIPWhenOptedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer func() {
		srv.Close()
		goleak.VerifyNone(t)
	}()

	opts := risor.Options{
		AllowInsecureHTTP:  true,
		AllowedHosts:       []string{loopbackHostname(srv.URL)},
		AllowPrivateEgress: true,
	}

	out, err := risor.Run(context.Background(), `gum_http_get("`+srv.URL+`/")`, opts)
	if err != nil {
		t.Fatalf("expected success with AllowPrivateEgress, got: %v", err)
	}
	m, ok := out.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", out.Value)
	}
	if sc, _ := m["status_code"].(int); sc != 200 {
		t.Errorf("status_code = %v, want 200", m["status_code"])
	}
}

// TestGumHTTPGetRejectsHostNotInAllowedHosts asserts that a host NOT in
// AllowedHosts (and not in the default allowlist) is denied with EGRESS_HOST_DENIED.
func TestGumHTTPGetRejectsHostNotInAllowedHosts(t *testing.T) {
	defer goleak.VerifyNone(t)

	// httptest server is allocated to simulate an available server, but we send
	// the request to a different host that is NOT in the allowlist.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opts := risor.Options{
		AllowedHosts: []string{"trusted.example.com"}, // server's actual host NOT included
	}

	_, err := risor.Run(context.Background(), `gum_http_get("https://other.example.com/")`, opts)
	if err == nil {
		t.Fatal("expected error for non-allowlisted host, got nil")
	}
	if !strings.Contains(err.Error(), "EGRESS_HOST_DENIED") {
		t.Errorf("error %q should contain 'EGRESS_HOST_DENIED'", err.Error())
	}
}

// TestGumHTTPGetRejectsNonHTTPSchemes asserts that ftp://, file://, and gopher://
// URLs are rejected before reaching the allowlist check, and the error message
// mentions "https" (consistent with the HTTPS-only enforcement).
func TestGumHTTPGetRejectsNonHTTPSchemes(t *testing.T) {
	defer goleak.VerifyNone(t)

	urls := []string{
		`gum_http_get("ftp://example.com/")`,
		`gum_http_get("file:///etc/passwd")`,
		`gum_http_get("gopher://example.com/")`,
	}

	for _, script := range urls {
		script := script
		t.Run(script, func(t *testing.T) {
			defer goleak.VerifyNone(t)

			opts := risor.Options{} // no AllowInsecureHTTP

			_, err := risor.Run(context.Background(), script, opts)
			if err == nil {
				t.Fatalf("expected error for non-http/https scheme in %q, got nil", script)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "https") {
				t.Errorf("error %q should mention 'https' (scheme rejection)", err.Error())
			}
		})
	}
}

// TestGumHTTPGetCaseInsensitiveHost asserts that explicit host entries match
// case-insensitively without depending on public DNS.
func TestGumHTTPGetCaseInsensitiveHost(t *testing.T) {
	defer goleak.VerifyNone(t)

	opts := risor.Options{
		AllowInsecureHTTP: true,
		AllowedHosts:      []string{"127.0.0.1"},
		HTTPTimeout:       100 * time.Millisecond,
	}

	_, err := risor.Run(context.Background(), `gum_http_get("http://127.0.0.1/")`, opts)
	if err == nil {
		t.Fatal("expected private-IP denial, got nil")
	}
	if err != nil && strings.Contains(err.Error(), "EGRESS_HOST_DENIED") {
		t.Errorf("error %q should NOT contain 'EGRESS_HOST_DENIED'; case-insensitive match should allow the host", err.Error())
	}
	if !strings.Contains(err.Error(), "EGRESS_PRIVATE_IP_DENIED") {
		t.Errorf("error %q should contain 'EGRESS_PRIVATE_IP_DENIED'", err.Error())
	}
}
