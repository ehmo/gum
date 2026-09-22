package main

// Wiring for the spec §13 managed-scope re-consent. internal/dispatch owns the
// approval and its verification, internal/mcp owns the elicitation wire, and
// neither may import internal/auth. This file supplies the missing third
// piece: the function that actually runs one Google consent.
//
// Only the MCP server wires it. `gum call` already has its own just-in-time
// login (jit_auth.go), which asks on the terminal the operator is sitting at.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/ehmo/gum/internal/auth"
	"github.com/ehmo/gum/internal/dispatch"
)

// newScopeUpgradeLogin returns the §13 consent runner for profile. Diagnostics
// and the authorization URL go to stderr, which on an MCP stdio server is the
// only channel that is not the protocol.
//
// The consent asks for exactly the scopes the approval named. Google's
// incremental authorization (include_granted_scopes=true, set by the login
// flow) returns the profile's other scopes alongside them, so a narrow ask
// does not narrow the stored grant.
func newScopeUpgradeLogin(profile string, stderr io.Writer) dispatch.ScopeUpgradeLogin {
	// One consent at a time: two concurrent tool calls would otherwise open
	// two browser windows and race to write the same keychain entry.
	var mu sync.Mutex

	return func(ctx context.Context, req dispatch.ScopeUpgradeRequest) (dispatch.ScopeUpgradeGrant, error) {
		mu.Lock()
		defer mu.Unlock()

		kb := auth.NewOSKeyring()
		client, ok, err := auth.LoadByoClient(kb, profile)
		if err != nil {
			return dispatch.ScopeUpgradeGrant{}, fmt.Errorf("read the registered OAuth client: %w", err)
		}
		if !ok {
			return dispatch.ScopeUpgradeGrant{}, errors.New("no OAuth client is registered for this profile; run `gum auth use-oauth-client`")
		}

		creds, lerr := interactiveByoLogin(ctx,
			auth.ByoOAuthConfig{
				ClientID:     client.ClientID,
				ClientSecret: client.ClientSecret,
				Profile:      profile,
				Scopes:       req.RequiredScopes,
				// A consent screen lets the operator untick scopes. The
				// approval named all of them, so a partial grant is refused
				// and nothing is stored.
				RequiredScopes: req.RequiredScopes,
				// The operator approved widening this profile, not moving it
				// to another account (§7, gum-q0kd).
				ExpectedSubject: req.ExpectedSubject,
			},
			newBrowserOpener(stderr, false, isHeadless),
		)
		if lerr != nil {
			return dispatch.ScopeUpgradeGrant{}, lerr
		}
		if rerr := recordExpectedSubject(profile, creds.StrategyName, creds.SubjectFingerprint); rerr != nil {
			_, _ = fmt.Fprintf(stderr, "gum: could not record the account fingerprint for this profile: %v\n", rerr)
		}

		// Report the stored grant, not the requested scopes: the kernel's
		// verification has to see what was actually written. Expansion matches
		// what the dispatcher's own allowlist was built from, so a scope a
		// broader grant subsumes is not reported missing.
		return dispatch.ScopeUpgradeGrant{
			GrantedScopes:          auth.ExpandGrantedScopes(auth.GrantedScopes(kb, profile)),
			AuthSubjectFingerprint: creds.SubjectFingerprint,
		}, nil
	}
}
