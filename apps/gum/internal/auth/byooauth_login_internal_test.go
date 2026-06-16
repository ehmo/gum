package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestByoOAuthLoginStoresRefreshToken pins the BYO interactive happy path:
// the loopback+PKCE flow against a user-supplied client obtains a refresh
// token, persists it under the scope-keyed entry, and returns the access
// token — with no managed-manifest gate and no gcloud.
func TestByoOAuthLoginStoresRefreshToken(t *testing.T) {
	keyring.MockInit()
	tokenSrv, _ := newFakeTokenServer(t)
	defer tokenSrv.Close()
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	defer authSrv.Close()

	scopes := []string{"https://www.googleapis.com/auth/webmasters.readonly"}
	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:      "user-client-id",
		Scopes:        scopes,
		TokenEndpoint: tokenSrv.URL,
		AuthEndpoint:  authSrv.URL,
	}, NewOSKeyring())
	b.client = tokenSrv.Client()
	b.BrowserOpener = func(authURL string) error {
		go followAuthURL(authURL)
		return nil
	}

	creds, err := b.Login(context.Background())
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if creds.Token != "at-1" {
		t.Errorf("access token = %q, want at-1", creds.Token)
	}
	if creds.StrategyName != "byo_oauth" {
		t.Errorf("strategy = %q, want byo_oauth", creds.StrategyName)
	}

	// The refresh token must be persisted under the per-client entry
	// Acquire/Resolve read, recording the granted scopes alongside it, so a
	// later call refreshes silently and subset requests match.
	stored, _ := b.kb.Get(b.keyringKey())
	var grant byoGrant
	if err := json.Unmarshal([]byte(stored), &grant); err != nil {
		t.Fatalf("stored grant is not JSON: %q (%v)", stored, err)
	}
	if grant.RefreshToken != "rt-1" {
		t.Errorf("stored refresh token = %q, want rt-1", grant.RefreshToken)
	}
	if len(grant.Scopes) != 1 || grant.Scopes[0] != scopes[0] {
		t.Errorf("stored granted scopes = %v, want %v", grant.Scopes, scopes)
	}
}

// TestByoOAuthLoginThenResolveRefreshesSilently pins that after a one-time
// Login, Resolve (the dispatch path) returns a freshly-refreshed access token
// from the stored refresh token without re-opening a browser.
func TestByoOAuthLoginThenResolveRefreshesSilently(t *testing.T) {
	keyring.MockInit()
	tokenSrv, refreshes := newFakeTokenServer(t)
	defer tokenSrv.Close()
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	defer authSrv.Close()

	scopes := []string{"https://www.googleapis.com/auth/webmasters.readonly"}
	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:      "user-client-id",
		Scopes:        scopes,
		TokenEndpoint: tokenSrv.URL,
		AuthEndpoint:  authSrv.URL,
	}, NewOSKeyring())
	b.client = tokenSrv.Client()
	b.BrowserOpener = func(authURL string) error { go followAuthURL(authURL); return nil }

	if _, err := b.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	// Force a non-cached resolve by clearing the in-memory credential.
	b.cached = nil
	creds, err := b.Resolve(context.Background(), scopes)
	if err != nil {
		t.Fatalf("Resolve after login: %v", err)
	}
	if creds.Token == "" {
		t.Error("Resolve returned empty access token")
	}
	if refreshes() == 0 {
		t.Error("expected a refresh_token grant after login, got none")
	}
}

// TestByoOAuthLoginPromptOffersAccountChooser pins that the authorization URL
// asks Google to BOTH force the consent screen (so a refresh_token is returned)
// AND show the account chooser (prompt=select_account). Without select_account
// Google silently reuses the single signed-in session, so clearing the local
// grant via `gum logout` and re-running `gum login` would re-grab the same
// Google account — making account switching impossible (gum-ocjx).
func TestByoOAuthLoginPromptOffersAccountChooser(t *testing.T) {
	keyring.MockInit()
	tokenSrv, _ := newFakeTokenServer(t)
	defer tokenSrv.Close()
	authSrv := newFakeAuthServer(t, "USE_GUM_STATE")
	defer authSrv.Close()

	b := NewByoOAuth(ByoOAuthConfig{
		ClientID:      "user-client-id",
		Scopes:        []string{"https://www.googleapis.com/auth/webmasters.readonly"},
		TokenEndpoint: tokenSrv.URL,
		AuthEndpoint:  authSrv.URL,
	}, NewOSKeyring())
	b.client = tokenSrv.Client()

	var gotPrompt string
	b.BrowserOpener = func(authURL string) error {
		if u, perr := url.Parse(authURL); perr == nil {
			gotPrompt = u.Query().Get("prompt")
		}
		go followAuthURL(authURL)
		return nil
	}

	if _, err := b.Login(context.Background()); err != nil {
		t.Fatalf("Login: %v", err)
	}
	// Pin the exact contract: select_account (account chooser, so operators can
	// switch Google accounts) AND consent (forces the consent screen, so Google
	// still returns a refresh_token). Exact match also catches an accidental
	// third prompt token slipping in.
	if gotPrompt != "select_account consent" {
		t.Errorf("prompt = %q; want exactly \"select_account consent\"", gotPrompt)
	}
}

// TestByoOAuthLoginRequiresClientID pins that Login refuses to start without a
// configured client_id, pointing the operator at `gum auth use-oauth-client`
// rather than failing deep in the flow.
func TestByoOAuthLoginRequiresClientID(t *testing.T) {
	b := NewByoOAuth(ByoOAuthConfig{Scopes: []string{"s"}}, NewOSKeyring())
	_, err := b.Login(context.Background())
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("Login: want *AuthError, got %T: %v", err, err)
	}
	if ae.SetupCommand != "gum auth use-oauth-client" {
		t.Errorf("SetupCommand = %q, want gum auth use-oauth-client", ae.SetupCommand)
	}
}

// TestByoOAuthLoginRequiresScopes pins that Login refuses an empty scope set —
// there is nothing to authorize.
func TestByoOAuthLoginRequiresScopes(t *testing.T) {
	b := NewByoOAuth(ByoOAuthConfig{ClientID: "id"}, NewOSKeyring())
	if _, err := b.Login(context.Background()); err == nil {
		t.Fatal("Login(no scopes) = nil, want error")
	}
}
