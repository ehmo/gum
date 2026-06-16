package auth

import (
	"context"
	"errors"
	"testing"

	keyringlib "github.com/zalando/go-keyring"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embedded"
)

func withManagedClient(t *testing.T, id, secret string) {
	t.Helper()
	origID, origSecret := embedded.GumOAuthClientID, embedded.GumOAuthClientSecret
	embedded.GumOAuthClientID = id
	embedded.GumOAuthClientSecret = secret
	t.Cleanup(func() {
		embedded.GumOAuthClientID = origID
		embedded.GumOAuthClientSecret = origSecret
	})
}

// byoVariant builds a byo_oauth ResolvedVariant carrying scope.
func byoVariant(scope string) *dispatch.ResolvedVariant {
	return &dispatch.ResolvedVariant{
		Variant: &catalog.Variant{
			AuthStrategy: catalog.AuthStrategyBYOOAuth,
			Scopes:       []string{scope},
		},
	}
}

// TestResolveAuthBYOClientNotConfigured pins that with no explicit BYO
// override and an empty keyring, the composite returns a typed
// BYO_OAUTH_CLIENT_NOT_CONFIGURED error that points the operator at
// `gum auth use-oauth-client` and echoes the required scopes — never an
// ADC/gcloud fallthrough.
func TestResolveAuthBYOClientNotConfigured(t *testing.T) {
	scope := "https://www.googleapis.com/auth/webmasters.readonly"
	c := &CompositeResolver{
		Keyring: &mockKeyring{data: map[string]string{}},
		Profile: DefaultAPIKeyProfile,
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, byoVariant(scope))
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "BYO_OAUTH_CLIENT_NOT_CONFIGURED" {
		t.Errorf("Code=%q; want BYO_OAUTH_CLIENT_NOT_CONFIGURED", ae.Code)
	}
	if ae.SetupCommand != "gum auth use-oauth-client" {
		t.Errorf("SetupCommand=%q; want gum auth use-oauth-client", ae.SetupCommand)
	}
	if len(ae.RequiredScopes) != 1 || ae.RequiredScopes[0] != scope {
		t.Errorf("RequiredScopes=%v; want [%s]", ae.RequiredScopes, scope)
	}
}

// TestResolveAuthBYOClientLoadFailed pins the keychain-read error arm: a
// locked/corrupt keychain surfaces BYO_OAUTH_CLIENT_LOAD_FAILED rather than
// being mistaken for "not configured" (which would wrongly prompt setup).
func TestResolveAuthBYOClientLoadFailed(t *testing.T) {
	c := &CompositeResolver{
		Keyring: errKeyring{getErr: errors.New("keychain locked")},
		Profile: DefaultAPIKeyProfile,
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{},
		byoVariant("https://www.googleapis.com/auth/webmasters.readonly"))
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "BYO_OAUTH_CLIENT_LOAD_FAILED" {
		t.Errorf("Code=%q; want BYO_OAUTH_CLIENT_LOAD_FAILED", ae.Code)
	}
}

// TestResolveAuthBYOConfiguredClientRequiresLogin pins the production wiring:
// a registered OAuth client (but no stored refresh token yet) builds a real
// ByoOAuth that returns NO_REFRESH_TOKEN enriched with the required scopes and
// the `gum login` setup command, so the JIT layer can prompt the user.
func TestResolveAuthBYOConfiguredClientRequiresLogin(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	scope := "https://www.googleapis.com/auth/webmasters.readonly"
	kb := NewOSKeyring()
	if err := StoreByoClient(kb, DefaultAPIKeyProfile, ByoClient{ClientID: "client-123"}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}
	c := &CompositeResolver{Keyring: kb, Profile: DefaultAPIKeyProfile}

	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, byoVariant(scope))
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "NO_REFRESH_TOKEN" {
		t.Errorf("Code=%q; want NO_REFRESH_TOKEN", ae.Code)
	}
	if ae.SetupCommand != "gum login" {
		t.Errorf("SetupCommand=%q; want gum login", ae.SetupCommand)
	}
	if len(ae.RequiredScopes) != 1 || ae.RequiredScopes[0] != scope {
		t.Errorf("RequiredScopes=%v; want [%s]", ae.RequiredScopes, scope)
	}
	if !ae.Retryable {
		t.Error("Retryable=false; want true (caller retries after login)")
	}
}

// TestResolveAuthIgnoresInjectedManagedClient pins the v1 auth posture: an
// injected managed OAuth client must not satisfy a byo_oauth variant. The
// operator must register their own OAuth client.
func TestResolveAuthIgnoresInjectedManagedClient(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)
	withManagedClient(t, "managed-id.apps.googleusercontent.com", "GOCSPX-managed-injected")

	scope := "https://www.googleapis.com/auth/webmasters.readonly"
	c := &CompositeResolver{Keyring: NewOSKeyring(), Profile: DefaultAPIKeyProfile}

	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, byoVariant(scope))
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "BYO_OAUTH_CLIENT_NOT_CONFIGURED" {
		t.Fatalf("Code=%q; want BYO_OAUTH_CLIENT_NOT_CONFIGURED", ae.Code)
	}
}

// TestResolveAuthRequiresBYOForAllScopes pins that every byo_oauth scope uses
// the same setup path. There is no managed-client allowlist carve-out in v1.
func TestResolveAuthRequiresBYOForAllScopes(t *testing.T) {
	withManagedClient(t, "managed-id.apps.googleusercontent.com", "GOCSPX-managed-injected")

	scope := "https://www.googleapis.com/auth/gmail.readonly"
	c := &CompositeResolver{
		Keyring: &mockKeyring{data: map[string]string{}},
		Profile: DefaultAPIKeyProfile,
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, byoVariant(scope))
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "BYO_OAUTH_CLIENT_NOT_CONFIGURED" {
		t.Fatalf("Code=%q; want BYO_OAUTH_CLIENT_NOT_CONFIGURED", ae.Code)
	}
}

// TestResolveAuthNoManagedClientStillNotConfigured pins the empty-client half:
// with no registered client, byo_oauth resolution returns
// BYO_OAUTH_CLIENT_NOT_CONFIGURED.
func TestResolveAuthNoManagedClientStillNotConfigured(t *testing.T) {
	withManagedClient(t, "managed-id.apps.googleusercontent.com", "") // empty secret = unavailable

	scope := "https://www.googleapis.com/auth/webmasters.readonly"
	c := &CompositeResolver{
		Keyring: &mockKeyring{data: map[string]string{}},
		Profile: DefaultAPIKeyProfile,
	}
	_, err := c.ResolveAuth(context.Background(), &dispatch.Invocation{}, byoVariant(scope))
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("err=%T %v; want *AuthError", err, err)
	}
	if ae.Code != "BYO_OAUTH_CLIENT_NOT_CONFIGURED" {
		t.Fatalf("Code=%q; want BYO_OAUTH_CLIENT_NOT_CONFIGURED", ae.Code)
	}
}

// TestCompositeKeyringDefaultsToOSKeyring pins the keyring() accessor default:
// a zero-value Keyring field falls back to the OS keychain rather than
// panicking on a nil backend.
func TestCompositeKeyringDefaultsToOSKeyring(t *testing.T) {
	c := &CompositeResolver{}
	if c.keyring() == nil {
		t.Error("keyring() returned nil; want OS keyring fallback")
	}
	if c.profile() != DefaultAPIKeyProfile {
		t.Errorf("profile()=%q; want %q", c.profile(), DefaultAPIKeyProfile)
	}
}
