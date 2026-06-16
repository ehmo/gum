package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// TestStoreLoginGrantUsesGrantedScopesNotAccumulated pins finding #6: after an
// account switch (same client keyring key, new refresh token), the stored grant
// records exactly the scopes Google granted and does NOT inflate them with the
// previous account's scopes — otherwise Acquire would refresh for a scope the
// new account never authorized and the API would return a silent 403.
func TestStoreLoginGrantUsesGrantedScopesNotAccumulated(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}
	// Prior account authorized scopeX, stored under the shared keyring key.
	prev := NewByoOAuth(ByoOAuthConfig{ClientID: "cid", Scopes: []string{"scopeX"}}, kb)
	if err := prev.StoreRefreshToken("rt-old"); err != nil {
		t.Fatalf("seed prior grant: %v", err)
	}
	// New login: a different account that granted only A and B.
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "cid", Scopes: []string{"scopeA"}}, kb)
	if err := b.storeLoginGrant("rt-new", "scopeA scopeB"); err != nil {
		t.Fatalf("storeLoginGrant: %v", err)
	}
	grant, ok, _ := b.loadGrant()
	if !ok {
		t.Fatal("no grant stored")
	}
	if slices.Contains(grant.Scopes, "scopeX") {
		t.Errorf("scope inflation: stored %v includes the prior account's scopeX", grant.Scopes)
	}
	if !slices.Contains(grant.Scopes, "scopeA") || !slices.Contains(grant.Scopes, "scopeB") {
		t.Errorf("stored scopes %v, want exactly [scopeA scopeB]", grant.Scopes)
	}
	if grant.RefreshToken != "rt-new" {
		t.Errorf("refresh token = %q, want rt-new", grant.RefreshToken)
	}
}

// TestStoreLoginGrantEmptyScopeUsesRequestedNotAccumulated pins the audit fix:
// when the server omits the granted scope, storeLoginGrant stores exactly the
// REQUESTED scopes — it must NOT fall back to the accumulating StoreRefreshToken,
// which would re-fold a previous (possibly different-account) grant's scopes and
// re-introduce the cross-account inflation -> silent-403 bug.
func TestStoreLoginGrantEmptyScopeUsesRequestedNotAccumulated(t *testing.T) {
	kb := &mockKeyring{data: map[string]string{}}
	prev := NewByoOAuth(ByoOAuthConfig{ClientID: "cid", Scopes: []string{"scopeX"}}, kb)
	if err := prev.StoreRefreshToken("rt-old"); err != nil {
		t.Fatalf("seed prior grant: %v", err)
	}
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "cid", Scopes: []string{"scopeA"}}, kb)
	if err := b.storeLoginGrant("rt-new", ""); err != nil {
		t.Fatalf("storeLoginGrant: %v", err)
	}
	grant, _, _ := b.loadGrant()
	if slices.Contains(grant.Scopes, "scopeX") {
		t.Errorf("empty-scope fallback inflated the grant with the prior account's scopeX: %v", grant.Scopes)
	}
	if len(grant.Scopes) != 1 || !slices.Contains(grant.Scopes, "scopeA") {
		t.Errorf("stored scopes = %v, want exactly [scopeA] (the requested set)", grant.Scopes)
	}
}

// TestAcquireOmitsClientSecretForPublicClient pins finding #5: a public PKCE
// client (no secret) must not send client_secret on token refresh (RFC 6749
// §2.3.1). The previous code sent client_secret="" unconditionally.
func TestAcquireOmitsClientSecretForPublicClient(t *testing.T) {
	var sentSecret, sawRequest bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_, sentSecret = r.PostForm["client_secret"]
		sawRequest = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600,"scope":"scopeA"}`))
	}))
	defer srv.Close()
	defer http.DefaultClient.CloseIdleConnections()

	kb := &mockKeyring{data: map[string]string{}}
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "cid", ClientSecret: "", Scopes: []string{"scopeA"}, TokenEndpoint: srv.URL}, kb)
	if err := b.StoreRefreshToken("rt"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}
	if _, err := b.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !sawRequest {
		t.Fatal("token endpoint never called")
	}
	if sentSecret {
		t.Error("public client sent client_secret on refresh; RFC 6749 §2.3.1 says it must be omitted")
	}
}

// TestAcquireSendsClientSecretForConfidentialClient pins the converse: a client
// configured with a secret still sends it.
func TestAcquireSendsClientSecretForConfidentialClient(t *testing.T) {
	var gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotSecret = r.PostForm.Get("client_secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","expires_in":3600,"scope":"scopeA"}`))
	}))
	defer srv.Close()
	defer http.DefaultClient.CloseIdleConnections()

	kb := &mockKeyring{data: map[string]string{}}
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "cid", ClientSecret: "shh", Scopes: []string{"scopeA"}, TokenEndpoint: srv.URL}, kb)
	if err := b.StoreRefreshToken("rt"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}
	if _, err := b.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if gotSecret != "shh" {
		t.Errorf("confidential client sent client_secret=%q, want shh", gotSecret)
	}
}
