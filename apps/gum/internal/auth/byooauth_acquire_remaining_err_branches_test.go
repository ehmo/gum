package auth_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// TestByoOAuthAcquireNewRequestFailureSurfacesAuthRefreshFailed pins
// the `http.NewRequestWithContext err → AUTH_REFRESH_FAILED` arm of
// Acquire (byooauth.go:111-117). Reached when TokenEndpoint is a
// malformed URL — e.g., one with an embedded control character that
// http.NewRequest's URL validation rejects. The wrap MUST surface
// "failed to build request" so operators can distinguish a misconfig
// (bad endpoint URL) from a downstream HTTP failure.
func TestByoOAuthAcquireNewRequestFailureSurfacesAuthRefreshFailed(t *testing.T) {
	kb := newMemKeyring()
	cfg := auth.ByoOAuthConfig{
		ClientID:     "x",
		ClientSecret: "y",
		Scopes:       []string{"openid"},
		// Embedded control character (\x7f DEL) trips http.NewRequest's
		// URL-validation regex deterministically.
		TokenEndpoint: "http://example.com/\x7ftoken",
	}
	byo := auth.NewByoOAuth(cfg, kb)
	if err := byo.StoreRefreshToken("rt"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	_, err := byo.Acquire(t.Context())
	if err == nil {
		t.Fatal("Acquire(bad url)=nil err; want AUTH_REFRESH_FAILED from NewRequest")
	}
	ae, ok := err.(*auth.AuthError)
	if !ok {
		t.Fatalf("err type=%T; want *auth.AuthError", err)
	}
	if ae.Code != "AUTH_REFRESH_FAILED" {
		t.Errorf("Code=%q; want AUTH_REFRESH_FAILED", ae.Code)
	}
	if !strings.Contains(ae.HumanRemediation, "failed to build request") {
		t.Errorf("HumanRemediation=%q; want 'failed to build request' (distinguish from HTTP-do failure)", ae.HumanRemediation)
	}
}

// TestByoOAuthAcquireOversizeBodySurfacesAuthRefreshFailed pins the
// `httputil.ReadCapped err → AUTH_REFRESH_FAILED` arm (byooauth.go:
// 133-139). Token-exchange bodies are tiny (~1 KiB); the 1 MiB cap
// (gum-4d66) defends against a hostile or misconfigured endpoint that
// streams gigabytes. The wrap surfaces "response too large" so
// operators see the defensive truncation rather than a confusing
// JSON-parse err downstream.
func TestByoOAuthAcquireOversizeBodySurfacesAuthRefreshFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// 1 MiB + 1 byte: ReadCapped's max+1 sentinel reports ErrResponseTooLarge.
		_, _ = w.Write(bytes.Repeat([]byte("a"), (1<<20)+1))
	}))
	t.Cleanup(srv.Close)

	kb := newMemKeyring()
	cfg := auth.ByoOAuthConfig{
		ClientID:      "x",
		ClientSecret:  "y",
		Scopes:        []string{"openid"},
		TokenEndpoint: srv.URL,
	}
	byo := auth.NewByoOAuth(cfg, kb)
	if err := byo.StoreRefreshToken("rt"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	_, err := byo.Acquire(t.Context())
	if err == nil {
		t.Fatal("Acquire(oversize body)=nil err; want AUTH_REFRESH_FAILED from ReadCapped")
	}
	ae, ok := err.(*auth.AuthError)
	if !ok {
		t.Fatalf("err type=%T; want *auth.AuthError", err)
	}
	if ae.Code != "AUTH_REFRESH_FAILED" {
		t.Errorf("Code=%q; want AUTH_REFRESH_FAILED", ae.Code)
	}
	if !strings.Contains(ae.HumanRemediation, "too large") {
		t.Errorf("HumanRemediation=%q; want 'too large' wrap", ae.HumanRemediation)
	}
}

// TestByoOAuthAcquireNon2xxBodyWithoutOAuthPatternFallbackMessage pins
// the `OAuthRemediation == "" → fallback "HTTP %d: %s"` arm
// (byooauth.go:143-145). Reached when the token endpoint returns
// non-2xx with a body that contains NEITHER "invalid_rapt" NOR
// "invalid_grant" — operators must still see status + body verbatim
// in the remediation field for triage, NOT the empty-string remediation.
func TestByoOAuthAcquireNon2xxBodyWithoutOAuthPatternFallbackMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("upstream maintenance window"))
	}))
	t.Cleanup(srv.Close)

	kb := newMemKeyring()
	cfg := auth.ByoOAuthConfig{
		ClientID:      "x",
		ClientSecret:  "y",
		Scopes:        []string{"openid"},
		TokenEndpoint: srv.URL,
	}
	byo := auth.NewByoOAuth(cfg, kb)
	if err := byo.StoreRefreshToken("rt"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	_, err := byo.Acquire(t.Context())
	if err == nil {
		t.Fatal("Acquire(500 + non-OAuth body)=nil err; want AUTH_REFRESH_FAILED fallback")
	}
	ae, ok := err.(*auth.AuthError)
	if !ok {
		t.Fatalf("err type=%T; want *auth.AuthError", err)
	}
	if ae.Code != "AUTH_REFRESH_FAILED" {
		t.Errorf("Code=%q; want AUTH_REFRESH_FAILED", ae.Code)
	}
	// Fallback must include both the status and the raw body for triage.
	if !strings.Contains(ae.HumanRemediation, "HTTP 500") {
		t.Errorf("HumanRemediation=%q; want 'HTTP 500' in fallback", ae.HumanRemediation)
	}
	if !strings.Contains(ae.HumanRemediation, "maintenance window") {
		t.Errorf("HumanRemediation=%q; want body verbatim in fallback", ae.HumanRemediation)
	}
}
