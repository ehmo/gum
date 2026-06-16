package auth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// TestByoOAuthAcquireCachedShortCircuits pins the
// `b.cached != nil && not-expired → return b.cached, nil` arm. After
// the first Acquire populates the cache, a second Acquire within the
// 30s buffer window MUST NOT hit the token endpoint again — that's the
// whole point of caching the access token (avoid spamming refresh
// every call).
func TestByoOAuthAcquireCachedShortCircuits(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	const fakeRefreshToken = "rt-cache-test"
	const fakeAccessToken = "at-cache-test"
	calls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + fakeAccessToken + `","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)

	kb := newMemKeyring()
	byo := auth.NewByoOAuth(auth.ByoOAuthConfig{
		ClientID:      "cid",
		ClientSecret:  "csecret",
		Scopes:        []string{"https://www.googleapis.com/auth/x"},
		TokenEndpoint: srv.URL,
	}, kb)
	if err := byo.StoreRefreshToken(fakeRefreshToken); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	// First call — populates b.cached.
	c1, err := byo.Acquire(t.Context())
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	// Second call — must short-circuit, returning the SAME *Credentials.
	c2, err := byo.Acquire(t.Context())
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}

	if calls != 1 {
		t.Errorf("token endpoint called %d times; want exactly 1 (second call must serve from b.cached)", calls)
	}
	if c1 != c2 {
		t.Error("cached short-circuit returned a different *Credentials pointer than the first call; want same pointer")
	}
}

// TestByoOAuthAcquireHTTPDoFailureSurfacesAuthRefreshFailed pins the
// `b.client.Do err → AUTH_REFRESH_FAILED` arm. Point TokenEndpoint at
// an httptest server that has been Close()'d so Do returns a connect
// refusal; the strategy MUST translate the transport error into the
// kernel's AUTH_REFRESH_FAILED contract rather than leaking the raw
// net.OpError to the caller (operators rely on the typed code to
// trigger re-auth flows).
func TestByoOAuthAcquireHTTPDoFailureSurfacesAuthRefreshFailed(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	endpoint := srv.URL
	srv.Close() // close immediately so subsequent Do() returns ECONNREFUSED

	kb := newMemKeyring()
	byo := auth.NewByoOAuth(auth.ByoOAuthConfig{
		ClientID:      "cid",
		ClientSecret:  "csecret",
		Scopes:        []string{"https://www.googleapis.com/auth/x"},
		TokenEndpoint: endpoint,
	}, kb)
	if err := byo.StoreRefreshToken("rt-http-do-fail"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}

	_, err := byo.Acquire(t.Context())
	if err == nil {
		t.Fatal("Acquire(closed endpoint)=nil err; want AUTH_REFRESH_FAILED")
	}
	ae, ok := err.(*auth.AuthError)
	if !ok {
		t.Fatalf("err type=%T; want *auth.AuthError", err)
	}
	if ae.Code != "AUTH_REFRESH_FAILED" {
		t.Errorf("AuthError.Code=%q; want AUTH_REFRESH_FAILED", ae.Code)
	}
	if !strings.Contains(ae.HumanRemediation, "HTTP request failed") {
		t.Errorf("HumanRemediation=%q; want 'HTTP request failed' substring", ae.HumanRemediation)
	}
}
