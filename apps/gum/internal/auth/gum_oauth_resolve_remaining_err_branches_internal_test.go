package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// errKeyring is a KeyringBackend that fails Get with a fixed err so
// Resolve's Vault.LookupRefreshToken err arm can be triggered without
// touching the real OS keychain. Mirrors the production
// AUTH_KEYCHAIN_UNAVAILABLE surface.
type errKeyring struct {
	getErr error
}

func (e errKeyring) Get(string) (string, error) { return "", e.getErr }
func (e errKeyring) Set(string, string) error   { return nil }
func (e errKeyring) Delete(string) error        { return nil }

// hitKeyring is a KeyringBackend that returns a fixed refresh token
// from Get — used to drive Resolve past the rt=="" arm into the
// exchangeRefresh call, where a fake token server can inject the
// next failure mode.
type hitKeyring struct {
	rt string
}

func (h hitKeyring) Get(string) (string, error) { return h.rt, nil }
func (h hitKeyring) Set(string, string) error   { return nil }
func (h hitKeyring) Delete(string) error        { return nil }

// TestGumOAuthResolveVaultLookupErrorPropagates pins Resolve's
// `Vault.LookupRefreshToken err → return nil, err` arm
// (gum_oauth.go:100-102). Reached when the OS keychain is locked /
// missing / corrupted — Resolve MUST surface the vault's err
// verbatim so callers see AUTH_KEYCHAIN_UNAVAILABLE rather than the
// AUTH_LOGIN_REQUIRED that a `rt == ""` would produce. The
// distinction matters: locked-keychain is recoverable by unlocking
// (`security unlock-keychain`), AUTH_LOGIN_REQUIRED prompts a fresh
// login that won't fix the underlying keychain access.
func TestGumOAuthResolveVaultLookupErrorPropagates(t *testing.T) {
	sentinel := errors.New("keychain locked")
	g := &GumOAuth{
		ManifestBody: []byte(passingScopeManifestForResolve),
		Vault:        NewCredentialVault(errKeyring{getErr: sentinel}),
	}
	_, err := g.Resolve(context.Background(), []string{"scope.a"})
	if err == nil {
		t.Fatal("Resolve(vault.Get errs)=nil err; want vault err propagation")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("err=%v; want errors.Is(err, sentinel) — Resolve must propagate verbatim", err)
	}
	// Must NOT be wrapped as AUTH_LOGIN_REQUIRED — that would mask the
	// keychain failure and send the operator down the wrong remediation path.
	var ae *AuthError
	if errors.As(err, &ae) && ae.Code == "AUTH_LOGIN_REQUIRED" {
		t.Errorf("err.Code=AUTH_LOGIN_REQUIRED; want raw keychain err (not masked as login-required)")
	}
}

// TestGumOAuthResolveExchangeRefreshErrorPropagates pins Resolve's
// `exchangeRefresh err → return nil, err` arm (gum_oauth.go:115-117).
// Reached when a stored refresh token exists but the token endpoint
// rejects the refresh exchange (network failure, revoked token,
// project disabled). The fake TokenURL returns 400 with no body so
// postToken's non-2xx arm surfaces an err that Resolve propagates
// without further wrapping — operators see the original token-endpoint
// surface (postToken err) rather than a generic "resolve failed".
func TestGumOAuthResolveExchangeRefreshErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	t.Cleanup(srv.Close)

	g := &GumOAuth{
		ManifestBody: []byte(passingScopeManifestForResolve),
		Vault:        NewCredentialVault(hitKeyring{rt: "stale-rt"}),
		TokenURL:     srv.URL,
	}
	_, err := g.Resolve(context.Background(), []string{"scope.a"})
	if err == nil {
		t.Fatal("Resolve(token endpoint 400)=nil err; want exchangeRefresh err propagation")
	}
	// postToken errors surface as gum_oauth-prefixed wrapped errs.
	if !strings.Contains(err.Error(), "gum_oauth") {
		t.Errorf("err=%q; want 'gum_oauth' prefix from postToken (proves Resolve didn't re-wrap)", err)
	}
}
