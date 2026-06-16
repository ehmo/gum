package auth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// TestByoOauthHTTPNon200 pins the non-2xx branch: the token endpoint
// returns 400 with a JSON error body; Acquire must surface a typed
// AUTH_REFRESH_FAILED AuthError with the OAuth-error-derived
// remediation rather than the raw status line.
func TestByoOauthHTTPNon200(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token expired"}`))
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
		t.Fatal("Acquire: nil error; want AUTH_REFRESH_FAILED")
	}
	ae, ok := err.(*auth.AuthError)
	if !ok {
		t.Fatalf("err type=%T; want *auth.AuthError", err)
	}
	if ae.Code != "AUTH_REFRESH_FAILED" {
		t.Errorf("Code=%q; want AUTH_REFRESH_FAILED", ae.Code)
	}
}

// TestByoOauthHTTPMalformedJSON pins the unmarshal-error branch: the
// endpoint returns 200 with a non-JSON body; Acquire must wrap the
// parse failure into AUTH_REFRESH_FAILED so the caller never sees a
// half-populated Credentials.
func TestByoOauthHTTPMalformedJSON(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not valid json"))
	}))
	t.Cleanup(srv.Close)

	kb := newMemKeyring()
	cfg := auth.ByoOAuthConfig{
		ClientID:      "x",
		ClientSecret:  "y",
		TokenEndpoint: srv.URL,
	}
	byo := auth.NewByoOAuth(cfg, kb)
	if err := byo.StoreRefreshToken("rt"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	_, err := byo.Acquire(t.Context())
	if err == nil {
		t.Fatal("Acquire: nil; want unmarshal error")
	}
	ae, ok := err.(*auth.AuthError)
	if !ok {
		t.Fatalf("err type=%T; want *auth.AuthError", err)
	}
	if ae.Code != "AUTH_REFRESH_FAILED" {
		t.Errorf("Code=%q; want AUTH_REFRESH_FAILED", ae.Code)
	}
	if !strings.Contains(ae.HumanRemediation, "parse") {
		t.Errorf("HumanRemediation=%q; want parse hint", ae.HumanRemediation)
	}
}
