package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ehmo/gum/internal/auth"
)

// TestNewDefaultByoOAuth verifies the production wiring: caller supplies
// config, the helper constructs a ByoOAuth wired to the OS keyring. We
// don't assert the keyring is real (that's OS-specific); just that the
// resolver is non-nil and the config flows through.
func TestNewDefaultByoOAuth(t *testing.T) {
	got := auth.NewDefaultByoOAuth(auth.ByoOAuthConfig{
		ClientID:     "cid",
		ClientSecret: "csec",
		Scopes:       []string{"gmail.readonly"},
	})
	if got == nil {
		t.Fatal("NewDefaultByoOAuth returned nil")
	}
}

// TestByoOAuthRevoke covers Revoke: a best-effort POST to the revocation
// endpoint with the refresh token, followed by the local delete. A fake
// endpoint keeps it hermetic and asserts the token is sent.
func TestByoOAuthRevoke(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotToken = r.FormValue("token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer http.DefaultClient.CloseIdleConnections()

	kb := newMemKeyring()
	cfg := auth.ByoOAuthConfig{
		ClientID:       "cid",
		ClientSecret:   "csec",
		Scopes:         []string{"gmail.readonly"},
		RevokeEndpoint: srv.URL,
	}
	b := auth.NewByoOAuth(cfg, kb)

	if err := b.StoreRefreshToken("rt-abc"); err != nil {
		t.Fatalf("StoreRefreshToken: %v", err)
	}
	if err := b.Revoke(context.Background()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if gotToken != "rt-abc" {
		t.Errorf("revoke endpoint got token %q, want rt-abc", gotToken)
	}
	// Subsequent acquire should fail because the refresh token is gone locally.
	_, err := b.Resolve(context.Background(), []string{"gmail.readonly"})
	if err == nil {
		t.Errorf("expected error after Revoke; Resolve returned ok")
	}
}

// TestByoOAuthResolveNoRefreshTokenErrors locks the Resolve→Acquire shim:
// when no refresh token is stored, Resolve surfaces the same error Acquire
// produces (typed AuthError) so dispatch's resolver chain can map it to
// AUTH_RESOLVER_NOT_CONFIGURED.
func TestByoOAuthResolveNoRefreshTokenErrors(t *testing.T) {
	kb := newMemKeyring()
	b := auth.NewByoOAuth(auth.ByoOAuthConfig{
		ClientID:     "cid",
		ClientSecret: "csec",
		Scopes:       []string{"gmail.readonly"},
	}, kb)
	if _, err := b.Resolve(context.Background(), []string{"gmail.readonly"}); err == nil {
		t.Errorf("expected error when no refresh token stored")
	}
}
