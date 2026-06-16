package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/auth"
)

// newRefreshServer returns a token endpoint that echoes the presented
// refresh_token back as the access token ("at-<rt>"), so a test can prove which
// stored grant a resolve refreshed from. The returned counter records how many
// refreshes were attempted.
func newRefreshServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken: "at-" + r.FormValue("refresh_token"),
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestByoOAuthBroadGrantSatisfiesNarrowResolve is the core gum-ergy fix: a broad
// `gum login` stores one grant, and a later narrow per-op call resolves from it
// without a fresh authorization (the keying is per-client, the granted scope
// set is a superset of the request).
func TestByoOAuthBroadGrantSatisfiesNarrowResolve(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	srv, hits := newRefreshServer(t)
	kb := newMemKeyring()
	const client = "c1.apps.googleusercontent.com"

	broad := auth.NewByoOAuth(auth.ByoOAuthConfig{
		ClientID:      client,
		Scopes:        []string{"https://www.googleapis.com/auth/webmasters", "https://www.googleapis.com/auth/gmail.readonly"},
		TokenEndpoint: srv.URL,
	}, kb)
	if err := broad.StoreRefreshToken("rt-broad"); err != nil {
		t.Fatalf("store broad grant: %v", err)
	}

	narrow := auth.NewByoOAuth(auth.ByoOAuthConfig{
		ClientID:      client,
		Scopes:        []string{"https://www.googleapis.com/auth/gmail.readonly"},
		TokenEndpoint: srv.URL,
	}, kb)
	creds, err := narrow.Resolve(context.Background(), nil)
	if err != nil {
		t.Fatalf("narrow resolve should reuse the broad grant, got: %v", err)
	}
	if creds.Token != "at-rt-broad" {
		t.Errorf("access token = %q; want one refreshed from rt-broad", creds.Token)
	}
	if hits.Load() != 1 {
		t.Errorf("token endpoint hit %d times; want exactly 1 refresh", hits.Load())
	}
}

// TestByoOAuthMissingScopeForcesReauth pins that a request for a scope the
// stored grant does NOT cover routes to re-authorization (NO_REFRESH_TOKEN)
// carrying the op's full required scopes — and never burns a refresh.
func TestByoOAuthMissingScopeForcesReauth(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	srv, hits := newRefreshServer(t)
	kb := newMemKeyring()
	const client = "c2"

	have := auth.NewByoOAuth(auth.ByoOAuthConfig{ClientID: client, Scopes: []string{"scope.a"}, TokenEndpoint: srv.URL}, kb)
	if err := have.StoreRefreshToken("rt-a"); err != nil {
		t.Fatalf("store: %v", err)
	}

	need := auth.NewByoOAuth(auth.ByoOAuthConfig{ClientID: client, Scopes: []string{"scope.a", "scope.z"}, TokenEndpoint: srv.URL}, kb)
	_, err := need.Acquire(context.Background())
	var ae *auth.AuthError
	if !errors.As(err, &ae) || ae.Code != "NO_REFRESH_TOKEN" {
		t.Fatalf("want NO_REFRESH_TOKEN for an uncovered scope, got %v", err)
	}
	if len(ae.RequiredScopes) != 2 {
		t.Errorf("RequiredScopes = %v; want the op's 2 scopes so re-consent covers them", ae.RequiredScopes)
	}
	if hits.Load() != 0 {
		t.Errorf("token endpoint hit %d times; want 0 (no refresh on an insufficient grant)", hits.Load())
	}
}

// TestByoOAuthGrantUnionAccumulates pins that two separate per-op
// authorizations (e.g. two JIT prompts) accumulate into one grant: the second
// store must not clobber the first, so a request spanning both scopes resolves.
func TestByoOAuthGrantUnionAccumulates(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	srv, _ := newRefreshServer(t)
	kb := newMemKeyring()
	const client = "c3"

	first := auth.NewByoOAuth(auth.ByoOAuthConfig{ClientID: client, Scopes: []string{"scope.a"}, TokenEndpoint: srv.URL}, kb)
	if err := first.StoreRefreshToken("rt-1"); err != nil {
		t.Fatalf("store first: %v", err)
	}
	second := auth.NewByoOAuth(auth.ByoOAuthConfig{ClientID: client, Scopes: []string{"scope.b"}, TokenEndpoint: srv.URL}, kb)
	if err := second.StoreRefreshToken("rt-2"); err != nil {
		t.Fatalf("store second: %v", err)
	}

	both := auth.NewByoOAuth(auth.ByoOAuthConfig{ClientID: client, Scopes: []string{"scope.a", "scope.b"}, TokenEndpoint: srv.URL}, kb)
	creds, err := both.Acquire(context.Background())
	if err != nil {
		t.Fatalf("accumulated grant should satisfy a+b, got: %v", err)
	}
	if creds.Token != "at-rt-2" {
		t.Errorf("access token = %q; want one refreshed from the latest rt-2", creds.Token)
	}
}

// TestByoOAuthPerClientIsolation pins that grants are scoped per client_id: a
// different OAuth client never inherits another's stored authorization.
func TestByoOAuthPerClientIsolation(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	srv, _ := newRefreshServer(t)
	kb := newMemKeyring()

	one := auth.NewByoOAuth(auth.ByoOAuthConfig{ClientID: "client-one", Scopes: []string{"scope.a"}, TokenEndpoint: srv.URL}, kb)
	if err := one.StoreRefreshToken("rt-1"); err != nil {
		t.Fatalf("store: %v", err)
	}

	two := auth.NewByoOAuth(auth.ByoOAuthConfig{ClientID: "client-two", Scopes: []string{"scope.a"}, TokenEndpoint: srv.URL}, kb)
	_, err := two.Acquire(context.Background())
	var ae *auth.AuthError
	if !errors.As(err, &ae) || ae.Code != "NO_REFRESH_TOKEN" {
		t.Fatalf("want NO_REFRESH_TOKEN for a different client_id, got %v", err)
	}
}
