package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// passingScopeManifestForResolve is a synthetic manifest body whose single
// scope satisfies the active_scope_rule, letting Resolve flow past
// canStartGumOAuth without contacting the real manifest.
const passingScopeManifestForResolve = `{
  "schema_version": 1,
  "client_policy": {
    "flow": "installed",
    "client_id": "fake-client-id"
  },
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

// TestGumOAuthResolveManifestInvalidWrapsAuthError pins the
// loadManagedScopesManifest error arm of Resolve: a bad manifest body
// surfaces GUM_OAUTH_MANIFEST_INVALID with the decode error echoed in
// HumanRemediation, NOT a bare error or nil credentials with no diag.
func TestGumOAuthResolveManifestInvalidWrapsAuthError(t *testing.T) {
	g := &GumOAuth{ManifestBody: []byte("{not json")}
	_, err := g.Resolve(context.Background(), []string{"scope.a"})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "GUM_OAUTH_MANIFEST_INVALID" {
		t.Errorf("Code=%q; want GUM_OAUTH_MANIFEST_INVALID", ae.Code)
	}
}

// TestGumOAuthResolveVaultNilSurfacesResolverNotConfigured pins the
// nil-Vault guard: a half-built GumOAuth (Vault unset) must NOT proceed
// to LookupRefreshToken and panic on the nil pointer; instead it must
// surface AUTH_RESOLVER_NOT_CONFIGURED so the wiring layer can repair.
func TestGumOAuthResolveVaultNilSurfacesResolverNotConfigured(t *testing.T) {
	g := &GumOAuth{
		ManifestBody: []byte(passingScopeManifestForResolve),
		Vault:        nil,
	}
	_, err := g.Resolve(context.Background(), []string{"scope.a"})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "AUTH_RESOLVER_NOT_CONFIGURED" {
		t.Errorf("Code=%q; want AUTH_RESOLVER_NOT_CONFIGURED", ae.Code)
	}
}

// TestGumOAuthResolveLoginRequiredWhenNoStoredToken pins the rt==""
// branch: the vault has no refresh token stored for these scopes, so
// Resolve must surface AUTH_LOGIN_REQUIRED with Retryable=true and the
// scopes echoed in RequiredScopes (so the operator can rerun login).
func TestGumOAuthResolveLoginRequiredWhenNoStoredToken(t *testing.T) {
	keyring.MockInit()
	g := &GumOAuth{
		ManifestBody: []byte(passingScopeManifestForResolve),
		Vault:        NewCredentialVault(&OSKeyring{}),
	}
	_, err := g.Resolve(context.Background(), []string{"scope.a"})
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "AUTH_LOGIN_REQUIRED" {
		t.Errorf("Code=%q; want AUTH_LOGIN_REQUIRED", ae.Code)
	}
	if !ae.Retryable {
		t.Errorf("Retryable=false; want true so callers can retry after login")
	}
	if len(ae.RequiredScopes) != 1 || ae.RequiredScopes[0] != "scope.a" {
		t.Errorf("RequiredScopes=%v; want [scope.a]", ae.RequiredScopes)
	}
}
