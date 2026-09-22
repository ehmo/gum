// Spec §7 "Compound auth token forwarding", cmd side. internal/plugins declares
// the seam; this file decides which credential the host actually forwards, so
// the token policy stays with the rest of the auth wiring instead of leaking
// into the plugin subsystem.

package main

import (
	"context"
	"fmt"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/plugins"
)

// googleTokenForwarder resolves the active Google access token for the profile
// it was built for. A compound plugin calls a Google API as the user, so it
// needs the access token and nothing else: the refresh token stays with gum.
type googleTokenForwarder struct{ profile string }

// newGoogleTokenForwarder builds the resolver a Host injects at spawn. It is
// attached unconditionally; the host consults it only for a manifest that
// declared the reserved name, so an ordinary plugin never touches the keychain.
func newGoogleTokenForwarder(profile string) plugins.GoogleTokenResolver {
	return googleTokenForwarder{profile: profile}
}

// ResolveGoogleToken acquires the token, asking for exactly the scopes the user
// already granted. Requesting the full catalog set instead would open a consent
// window in the middle of a plugin spawn, and a spawn has no terminal to answer
// one.
//
// Every failure is returned rather than swallowed. The plugin is promised a
// live token, and one that starts without it fails its first upstream call with
// a 401 that names neither the cause nor the fix.
func (f googleTokenForwarder) ResolveGoogleToken(ctx context.Context) (plugins.GoogleToken, error) {
	scopes := forwardingGrantedScopes(f.profile)
	if len(scopes) == 0 {
		return plugins.GoogleToken{}, &auth.AuthError{
			Code:             "AUTH_REQUIRED",
			Strategy:         "compound",
			SetupCommand:     "gum login",
			HumanRemediation: fmt.Sprintf("profile %q has granted no Google scopes, so there is no token to forward", f.profile),
			UserMessage:      "Run `gum login` before starting a plugin that calls Google on your behalf.",
		}
	}

	resolver, err := newForwardingAuthResolver(f.profile, scopes)
	if err != nil {
		return plugins.GoogleToken{}, err
	}

	creds, err := resolver.Resolve(ctx, scopes)
	if err != nil {
		return plugins.GoogleToken{}, err
	}
	if creds == nil || creds.Token == "" {
		return plugins.GoogleToken{}, &auth.AuthError{
			Code:             "AUTH_REQUIRED",
			Strategy:         "compound",
			SetupCommand:     "gum login",
			RequiredScopes:   scopes,
			HumanRemediation: fmt.Sprintf("the resolver returned no access token for profile %q", f.profile),
			UserMessage:      "Run `gum login` before starting a plugin that calls Google on your behalf.",
		}
	}

	return plugins.GoogleToken{
		AccessToken:        creds.Token,
		SubjectFingerprint: creds.SubjectFingerprint,
		Scopes:             creds.Scopes,
	}, nil
}

// forwardingGrantedScopes reports the scopes the profile has already consented
// to. A locked keychain yields none, which surfaces as AUTH_REQUIRED naming
// `gum login` rather than as a spawn that stalls on an interactive prompt. It
// is a var so a test can forward a token without a real keychain.
var forwardingGrantedScopes = func(profile string) []string {
	return auth.ExpandGrantedScopes(auth.GrantedScopes(auth.NewBestEffortOSKeyring(), profile))
}

// newForwardingAuthResolver picks the strategy the same way `gum auth probe
// --strategy auto` does: the operator's own OAuth client when one is
// registered, ADC otherwise. It is a var so a test can forward a token it
// controls without a keychain.
var newForwardingAuthResolver = func(profile string, scopes []string) (auth.Resolver, error) {
	client, ok, err := auth.LoadByoClient(auth.NewOSKeyring(), profile)
	if err != nil {
		return nil, fmt.Errorf("plugin token forwarding: read OAuth client from keychain: %w", err)
	}
	if !ok {
		return auth.NewLiveADCResolver(), nil
	}

	return auth.NewDefaultByoOAuth(auth.ByoOAuthConfig{
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		Profile:      profile,
		Scopes:       scopes,
	}), nil
}
