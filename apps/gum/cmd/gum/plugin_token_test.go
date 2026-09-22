package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auth"
	"github.com/zalando/go-keyring"
)

// stubAuthResolver answers Resolve with whatever the test set.
type stubAuthResolver struct {
	creds *auth.Credentials
	err   error
}

func (s stubAuthResolver) Resolve(context.Context, []string) (*auth.Credentials, error) {
	return s.creds, s.err
}

// stubForwardingSeams replaces the two package seams for the duration of a test
// so no case reaches the operator's real keychain.
func stubForwardingSeams(t *testing.T, scopes []string, res auth.Resolver, factoryErr error) {
	t.Helper()
	origScopes, origResolver := forwardingGrantedScopes, newForwardingAuthResolver
	t.Cleanup(func() {
		forwardingGrantedScopes, newForwardingAuthResolver = origScopes, origResolver
	})
	forwardingGrantedScopes = func(string) []string { return scopes }
	newForwardingAuthResolver = func(string, []string) (auth.Resolver, error) {
		return res, factoryErr
	}
}

// TestGoogleTokenForwarderCarriesCredential is the happy path: the resolved
// token, its subject and its scopes reach the plugin host unchanged.
func TestGoogleTokenForwarderCarriesCredential(t *testing.T) {
	granted := []string{"https://www.googleapis.com/auth/adwords"}
	stubForwardingSeams(t, granted, stubAuthResolver{creds: &auth.Credentials{
		Token:              "ya29.live",
		SubjectFingerprint: "sha256:abc",
		Scopes:             granted,
	}}, nil)

	tok, err := newGoogleTokenForwarder("default").ResolveGoogleToken(context.Background())
	if err != nil {
		t.Fatalf("ResolveGoogleToken: %v", err)
	}
	if tok.AccessToken != "ya29.live" {
		t.Errorf("AccessToken = %q; want ya29.live", tok.AccessToken)
	}
	if tok.SubjectFingerprint != "sha256:abc" {
		t.Errorf("SubjectFingerprint = %q; want sha256:abc", tok.SubjectFingerprint)
	}
	if len(tok.Scopes) != 1 || tok.Scopes[0] != granted[0] {
		t.Errorf("Scopes = %v; want %v", tok.Scopes, granted)
	}
}

// TestGoogleTokenForwarderRequiresLogin covers the profile that never logged
// in. The error names `gum login`, because a spawn has no terminal to run a
// consent flow on and the operator needs to be told what to do next.
func TestGoogleTokenForwarderRequiresLogin(t *testing.T) {
	stubForwardingSeams(t, nil, nil, nil)

	_, err := newGoogleTokenForwarder("default").ResolveGoogleToken(context.Background())
	if err == nil {
		t.Fatal("ResolveGoogleToken = nil error; want AUTH_REQUIRED")
	}
	var authErr *auth.AuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("err = %T; want *auth.AuthError", err)
	}
	if authErr.Code != "AUTH_REQUIRED" {
		t.Errorf("Code = %q; want AUTH_REQUIRED", authErr.Code)
	}
	if authErr.SetupCommand != "gum login" {
		t.Errorf("SetupCommand = %q; want `gum login`", authErr.SetupCommand)
	}
}

// TestGoogleTokenForwarderEmptyTokenIsAuthRequired: a resolver that reports
// success but hands back nothing must not become a silent no-token spawn.
func TestGoogleTokenForwarderEmptyTokenIsAuthRequired(t *testing.T) {
	cases := map[string]*auth.Credentials{
		"nil credentials":   nil,
		"blank token field": {Token: ""},
	}
	for name, creds := range cases {
		t.Run(name, func(t *testing.T) {
			stubForwardingSeams(t, []string{"s"}, stubAuthResolver{creds: creds}, nil)

			_, err := newGoogleTokenForwarder("default").ResolveGoogleToken(context.Background())
			var authErr *auth.AuthError
			if !errors.As(err, &authErr) || authErr.Code != "AUTH_REQUIRED" {
				t.Fatalf("err = %v; want AUTH_REQUIRED", err)
			}
		})
	}
}

// TestGoogleTokenForwarderSurfacesResolverError keeps an acquisition failure
// visible instead of degrading to an unauthenticated spawn.
func TestGoogleTokenForwarderSurfacesResolverError(t *testing.T) {
	stubForwardingSeams(t, []string{"s"},
		stubAuthResolver{err: errors.New("refresh rejected")}, nil)

	_, err := newGoogleTokenForwarder("default").ResolveGoogleToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refresh rejected") {
		t.Fatalf("err = %v; want the resolver failure surfaced", err)
	}
}

// TestGoogleTokenForwarderSurfacesFactoryError covers the keychain read that
// fails before any resolver exists.
func TestGoogleTokenForwarderSurfacesFactoryError(t *testing.T) {
	stubForwardingSeams(t, []string{"s"}, nil, errors.New("keychain locked"))

	_, err := newGoogleTokenForwarder("default").ResolveGoogleToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "keychain locked") {
		t.Fatalf("err = %v; want the factory failure surfaced", err)
	}
}

// TestNewForwardingAuthResolverFallsBackToADC exercises the real factory on a
// profile with no registered OAuth client. It must return a usable resolver
// rather than an error, which is what makes an ADC-only host work.
func TestNewForwardingAuthResolverFallsBackToADC(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)

	res, err := newForwardingAuthResolver("gumtest-no-client", []string{"s"})
	if err != nil {
		t.Fatalf("newForwardingAuthResolver: %v", err)
	}
	if res == nil {
		t.Fatal("resolver = nil; want the ADC fallback")
	}
}

// TestNewForwardingAuthResolverUsesRegisteredClient: once the operator has
// registered their own Desktop OAuth client, forwarding goes through it and
// not through ADC.
func TestNewForwardingAuthResolverUsesRegisteredClient(t *testing.T) {
	keyring.MockInit()
	t.Cleanup(keyring.MockInit)
	if err := auth.StoreByoClient(auth.NewOSKeyring(), "gumtest-with-client", auth.ByoClient{
		ClientID:     "cid.apps.googleusercontent.com",
		ClientSecret: "csecret",
	}); err != nil {
		t.Fatalf("StoreByoClient: %v", err)
	}

	res, err := newForwardingAuthResolver("gumtest-with-client", []string{"s"})
	if err != nil {
		t.Fatalf("newForwardingAuthResolver: %v", err)
	}
	if res == nil {
		t.Fatal("resolver = nil; want the BYO OAuth resolver")
	}
}
