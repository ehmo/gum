// Spec §7 "Compound auth token forwarding". A plugin that brokers a Google API
// on the user's behalf needs the user's access token, but must never hold the
// refresh token that mints it. This file owns the whole exchange: the seam the
// host resolves the live token through, the install-time gate that decides
// which manifests may ask for it, and the audit record every forward leaves.

package plugins

import (
	"context"
	"fmt"

	"github.com/ehmo/gum/internal/catalog"
)

// forwardedTokenEnvName is the env var the subprocess reads. The manifest asks
// for it under the lowercase reservedCompoundEnvName; the two spellings are
// deliberately different so a manifest cannot reach the injected value by
// listing it in declared_capabilities.env_allow and having the host pass
// through whatever the operator's shell exported.
const forwardedTokenEnvName = "GOOGLE_ACCESS_TOKEN"

// tokenForwardedEvent is the §7 audit event name. It is emitted once per spawn
// that forwards a token, never per call: the subprocess keeps the value for its
// whole lifetime, so a per-call row would claim an exchange that never happened.
const tokenForwardedEvent = "plugin_token_forwarded"

// GoogleToken is the material the host forwards, plus the two non-secret facts
// the audit record needs. It is not auth.Credentials: a plugin has no business
// with an API key, a quota project, or an expiry it cannot act on.
type GoogleToken struct {
	// AccessToken is the Bearer value. An empty token means "no active
	// session"; the host then forwards nothing rather than an empty string.
	AccessToken string
	// SubjectFingerprint identifies which principal the token belongs to,
	// without naming them. It is what makes the audit row answerable.
	SubjectFingerprint string
	// Scopes is the granted scope list the token carries, so an operator
	// reading the audit log can see the reach a plugin was handed.
	Scopes []string
}

// GoogleTokenResolver produces the host's active Google access token at spawn.
//
// The interface exists so internal/plugins does not reach into the auth stack
// to decide which strategy, profile, or scope set applies. cmd builds the
// concrete resolver and injects it on HostConfig, which keeps the token policy
// in one place and lets a test spawn a plugin with a token it controls.
type GoogleTokenResolver interface {
	ResolveGoogleToken(ctx context.Context) (GoogleToken, error)
}

// AuditSink carries the §7 forwarding record into the profile audit log. cmd
// wires it to internal/auditlog; this package must not import that package,
// because §14 lets a layer call only the layer directly below it.
type AuditSink interface {
	Append(entry map[string]any)
}

// ValidateCompoundTokenDeclaration is §7's install gate: a manifest may ask for
// the forwarded token only when every advertised tool is auth_strategy
// "compound".
//
// The scope is the whole subprocess, not one tool. One env block serves every
// tool the plugin advertises, so a single non-compound tool sharing that
// process would read a token §7 says only compound tools may receive. There is
// no way to scope an env var to one tool, so the only enforceable rule is
// all-or-nothing. A plugin that mixes strategies must ship as two plugins.
//
// A manifest that never asks for the token is unaffected, which is what keeps
// every existing plugin valid at manifest_schema_version 1.
func ValidateCompoundTokenDeclaration(pluginID string, needs []string, tools []ToolDecl) error {
	if !declaresCompoundToken(needs) {
		return nil
	}

	if len(tools) == 0 {
		return fmt.Errorf("%w: plugin '%s' requests the forwarded Google token but advertises no tools",
			ErrPluginEnvProhibited, pluginID)
	}

	for _, t := range tools {
		if t.AuthStrategy == catalog.AuthStrategyCompound {
			continue
		}
		return fmt.Errorf("%w: plugin '%s' requests the forwarded Google token, but tool '%s' declares auth_strategy '%s'; every advertised tool must be '%s'",
			ErrPluginEnvProhibited, pluginID, t.Name, t.AuthStrategy, catalog.AuthStrategyCompound)
	}

	return nil
}

// declaresCompoundToken reports whether the manifest asked for the forwarded
// token by its reserved lowercase name.
func declaresCompoundToken(needs []string) bool {
	for _, n := range needs {
		if n == reservedCompoundEnvName {
			return true
		}
	}
	return false
}

// resolveForwardedToken asks the injected resolver for the host's active token
// and builds the §7 audit entry that accompanies it.
//
// A nil resolver, or a resolver with no active session, yields no token and no
// entry. The host then leaves the reserved name unset rather than substituting
// a stored secret or an ambient export: the plugin is promised the host's live
// token and cannot tell a stale string from a real one, so it would spend the
// wrong value and surface the failure as an opaque upstream 401.
//
// A resolver that fails outright is different from one with no session, and
// stops the spawn.
func resolveForwardedToken(ctx context.Context, pluginID string, res GoogleTokenResolver) (string, map[string]any, error) {
	if res == nil {
		return "", nil, nil
	}

	tok, err := res.ResolveGoogleToken(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("plugin start: resolve forwarded Google token for '%s': %w", pluginID, err)
	}
	if tok.AccessToken == "" {
		return "", nil, nil
	}

	// The scope slice is copied: the entry outlives this call, and a later
	// token refresh reusing the resolver's backing array would rewrite a row
	// the audit log has already accepted.
	entry := map[string]any{
		"event":               tokenForwardedEvent,
		"plugin":              pluginID,
		"subject_fingerprint": tok.SubjectFingerprint,
		"scopes":              append([]string(nil), tok.Scopes...),
	}

	return tok.AccessToken, entry, nil
}
