package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// passingScopeManifestForLogin satisfies active_scope_rule but omits
// client_id so the Login flow trips the GUM_OAUTH_CLIENT_ID_MISSING guard.
const passingScopeManifestForLogin = `{
  "schema_version": 1,
  "client_policy": {"flow": "installed"},
  "scopes": [
    {
      "scope": "scope.a",
      "status": "active",
      "verification_state": "verified",
      "project_evidence_state": "ready",
      "live_canary_state": "passing"
    }
  ]
}`

// TestGumOAuthLoginManifestInvalidWrapsAuthError pins the
// loadManagedScopesManifest error arm of Login: malformed manifest
// bytes must surface GUM_OAUTH_MANIFEST_INVALID before any network or
// vault touch.
func TestGumOAuthLoginManifestInvalidWrapsAuthError(t *testing.T) {
	g := &GumOAuth{ManifestBody: []byte("{not json")}
	_, err := g.Login(context.Background(), []string{"scope.a"})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "GUM_OAUTH_MANIFEST_INVALID" {
		t.Errorf("Code=%q; want GUM_OAUTH_MANIFEST_INVALID", ae.Code)
	}
}

// TestGumOAuthLoginVaultNilSurfacesResolverNotConfigured pins the
// nil-Vault guard for Login: half-built strategy must NOT proceed to
// the loopback listener; surface AUTH_RESOLVER_NOT_CONFIGURED.
func TestGumOAuthLoginVaultNilSurfacesResolverNotConfigured(t *testing.T) {
	g := &GumOAuth{
		ManifestBody: []byte(passingScopeManifestForLogin),
		Vault:        nil,
	}
	_, err := g.Login(context.Background(), []string{"scope.a"})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "AUTH_RESOLVER_NOT_CONFIGURED" {
		t.Errorf("Code=%q; want AUTH_RESOLVER_NOT_CONFIGURED", ae.Code)
	}
}

// TestGumOAuthLoginClientIDMissingSurfacesError pins the
// clientID=="" guard: a manifest without a client_id (and no
// ClientIDOverride) means PKCE cannot start. Login must reject with
// GUM_OAUTH_CLIENT_ID_MISSING so operators understand the manifest is
// underspecified rather than the OAuth provider unreachable.
func TestGumOAuthLoginClientIDMissingSurfacesError(t *testing.T) {
	keyring.MockInit()
	g := &GumOAuth{
		ManifestBody: []byte(passingScopeManifestForLogin),
		Vault:        NewCredentialVault(&OSKeyring{}),
	}
	_, err := g.Login(context.Background(), []string{"scope.a"})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "GUM_OAUTH_CLIENT_ID_MISSING" {
		t.Errorf("Code=%q; want GUM_OAUTH_CLIENT_ID_MISSING", ae.Code)
	}
}
