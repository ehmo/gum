package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestPostTokenDecodeErrorWraps pins the json.NewDecoder error arm of
// postToken: when the token endpoint returns non-JSON bytes (e.g. an
// HTML error page from a load balancer), the helper must surface the
// 'decode token response' wrap with the HTTP status code echoed.
func TestPostTokenDecodeErrorWraps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer srv.Close()

	g := &GumOAuth{TokenURL: srv.URL}
	_, err := g.postToken(context.Background(), url.Values{"grant_type": {"refresh_token"}})
	if err == nil {
		t.Fatal("want decode error; got nil")
	}
	if !strings.Contains(err.Error(), "decode token response") {
		t.Errorf("err=%v; want 'decode token response' wrap", err)
	}
}

// TestPostTokenHTTP4xxSurfacesAuthError pins the resp.StatusCode>=400
// arm: a 4xx response (e.g. 400 invalid_grant from Google) must wrap
// as GUM_OAUTH_TOKEN_EXCHANGE_FAILED with the error and error_description
// fields echoed into HumanRemediation so the operator can diagnose
// without enabling -v logging.
func TestPostTokenHTTP4xxSurfacesAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh expired"}`))
	}))
	defer srv.Close()

	g := &GumOAuth{TokenURL: srv.URL}
	_, err := g.postToken(context.Background(), url.Values{"grant_type": {"refresh_token"}})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "GUM_OAUTH_TOKEN_EXCHANGE_FAILED" {
		t.Errorf("Code=%q; want GUM_OAUTH_TOKEN_EXCHANGE_FAILED", ae.Code)
	}
	if !strings.Contains(ae.HumanRemediation, "400") || !strings.Contains(ae.HumanRemediation, "invalid_grant") {
		t.Errorf("HumanRemediation=%q; want '400' + 'invalid_grant' echoed", ae.HumanRemediation)
	}
}

// TestPostTokenTransportErrorWraps pins the httpClient().Do error arm:
// pointing the helper at an immediately-closed listener simulates a
// dial failure (connection refused). The wrap must include 'token POST'
// so the upstream caller can distinguish dial vs. response-decode bugs.
func TestPostTokenTransportErrorWraps(t *testing.T) {
	// Bring up and immediately close so the URL points at a dead port.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	deadURL := srv.URL
	srv.Close()

	g := &GumOAuth{TokenURL: deadURL}
	_, err := g.postToken(context.Background(), url.Values{"grant_type": {"refresh_token"}})
	if err == nil {
		t.Fatal("want transport error; got nil")
	}
	if !strings.Contains(err.Error(), "token POST") {
		t.Errorf("err=%v; want 'token POST' wrap", err)
	}
}
