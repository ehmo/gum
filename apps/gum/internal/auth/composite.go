package auth

import (
	"context"
	"fmt"

	"github.com/ehmo/gum/internal/dispatch"
)

// CompositeResolver implements dispatch.AuthResolver by looking at the variant's
// auth_strategy and delegating to the matching per-strategy resolver. v1 wires
// byo_oauth, adc, api_key, service_account, and dormant gum_oauth internals
// gated by the managed-scopes manifest; the remaining strategies return
// AUTH_STRATEGY_NOT_IMPLEMENTED.
type CompositeResolver struct {
	// BYO is an explicit override for the byo_oauth resolver. When nil the
	// composite builds one from the OAuth client the operator registered via
	// `gum auth use-oauth-client` (read from Keyring under Profile). Tests set
	// this to inject a stub; production leaves it nil.
	BYO    Resolver // optional; resolves byo_oauth
	ADC    Resolver // optional; resolves adc
	APIKey Resolver // optional; resolves api_key (reads GUM_API_KEY in default wiring)
	// Keyring is the backend the byo_oauth path reads the registered OAuth
	// client from. Nil falls back to the OS keychain (NewOSKeyring).
	Keyring KeyringBackend
	// Profile names the credential profile for the byo_oauth client lookup.
	// Empty falls back to DefaultAPIKeyProfile.
	Profile string
	// SA resolves service_account_key by minting tokens from a downloaded
	// JSON key file. Nil means "operator has not configured a key" — the
	// composite returns AUTH_RESOLVER_NOT_CONFIGURED in that branch with
	// a hint pointing at gum auth use-service-account.
	SA Resolver
	// GumOAuth resolves auth_strategy=gum_oauth. Nil means "manifest gate
	// will be evaluated lazily" — the composite still returns a typed
	// error, just with GUM_OAUTH_MANAGED_CLIENT_NOT_READY when no scope is
	// promoted (the v0.1.0 default).
	GumOAuth Resolver
}

// NewDefaultCompositeResolver wires the production composite: live ADC fetch
// via golang.org/x/oauth2/google, the env-var API key reader, and the
// keychain-backed byo_oauth path. The byo_oauth resolver is built lazily from
// the OAuth client the operator registered via `gum auth use-oauth-client`; no
// gcloud and no ADC fallthrough.
func NewDefaultCompositeResolver() *CompositeResolver {
	c := &CompositeResolver{
		ADC:      NewLiveADCResolver(),
		APIKey:   NewAPIKeyResolver(),
		GumOAuth: NewGumOAuth(),
		Keyring:  NewOSKeyring(),
		Profile:  DefaultAPIKeyProfile,
	}
	// SA wiring is opt-in: a bogus GUM_SERVICE_ACCOUNT_KEY must not crash
	// gum startup. Failure to construct the resolver leaves c.SA nil and
	// the composite returns AUTH_RESOLVER_NOT_CONFIGURED at dispatch time
	// — the same path as "operator never set the env var".
	if sa, err := NewServiceAccountResolverFromEnv(); err == nil {
		c.SA = sa
	}
	return c
}

// keyring returns the configured keyring backend, defaulting to the OS keychain.
func (c *CompositeResolver) keyring() KeyringBackend {
	if c.Keyring != nil {
		return c.Keyring
	}
	return NewOSKeyring()
}

// profile returns the configured credential profile, defaulting to "default".
func (c *CompositeResolver) profile() string {
	if c.Profile != "" {
		return c.Profile
	}
	return DefaultAPIKeyProfile
}

// byoResolver returns the byo_oauth resolver for this request. An explicit
// c.BYO (a test/override hook) wins; otherwise the resolver is built from the
// OAuth client the operator registered via `gum auth use-oauth-client`, keyed
// by profile in the OS keychain. When no client is configured the caller gets
// a typed BYO_OAUTH_CLIENT_NOT_CONFIGURED error pointing at the setup command
// — there is deliberately NO ADC/gcloud fallthrough.
func (c *CompositeResolver) byoResolver(scopeNames []string) (Resolver, error) {
	if c.BYO != nil {
		return c.BYO, nil
	}
	client, ok, err := LoadByoClient(c.keyring(), c.profile())
	if err != nil {
		return nil, &AuthError{
			Code:             "BYO_OAUTH_CLIENT_LOAD_FAILED",
			Strategy:         "byo_oauth",
			HumanRemediation: fmt.Sprintf("could not read the stored OAuth client from the keychain: %v", err),
		}
	}
	if !ok {
		return nil, &AuthError{
			Code:             "BYO_OAUTH_CLIENT_NOT_CONFIGURED",
			Strategy:         "byo_oauth",
			SetupCommand:     "gum auth use-oauth-client",
			RequiredScopes:   scopeNames,
			HumanRemediation: "no OAuth client configured; create a Desktop OAuth client in the Google Cloud console, then run `gum auth use-oauth-client --client-id <id> --secret-stdin`",
			UserMessage:      "Register your Google OAuth client with `gum auth use-oauth-client`, then run `gum login`.",
		}
	}
	return NewDefaultByoOAuth(ByoOAuthConfig{
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		Profile:      c.profile(),
		Scopes:       scopeNames,
	}), nil
}

// ResolveAuth is the dispatch.AuthResolver entry point. It looks at the
// variant's auth_strategy and routes to the configured per-strategy resolver.
func (c *CompositeResolver) ResolveAuth(ctx context.Context, inv *dispatch.Invocation, rv *dispatch.ResolvedVariant) (*dispatch.Credentials, error) {
	if rv == nil || rv.Variant == nil {
		return nil, nil
	}
	strat, err := strategyFromCatalog(rv.Variant.AuthStrategy)
	if err != nil {
		return nil, err
	}

	scopeNames := append([]string{}, rv.Variant.Scopes...)

	switch strat {
	case StrategyADC:
		if c.ADC == nil {
			return nil, &AuthError{
				Code:             "AUTH_RESOLVER_NOT_CONFIGURED",
				Strategy:         "adc",
				HumanRemediation: "ADC resolver not wired on this dispatcher",
			}
		}
		creds, err := c.ADC.Resolve(ctx, scopeNames)
		if err != nil {
			return nil, err
		}
		return creds.ToDispatchCredentials(), nil

	case StrategyBYOOAuth:
		byoR, err := c.byoResolver(scopeNames)
		if err != nil {
			return nil, err
		}
		creds, err := byoR.Resolve(ctx, scopeNames)
		if err != nil {
			return nil, err
		}
		return creds.ToDispatchCredentials(), nil

	case StrategyServiceAccountKey:
		if c.SA == nil {
			return nil, &AuthError{
				Code:             "AUTH_RESOLVER_NOT_CONFIGURED",
				Strategy:         "service_account_key",
				HumanRemediation: "set GUM_SERVICE_ACCOUNT_KEY to a downloaded SA JSON key path (run `gum auth use-service-account <key.json>` for the export line)",
			}
		}
		creds, err := c.SA.Resolve(ctx, scopeNames)
		if err != nil {
			return nil, err
		}
		return creds.ToDispatchCredentials(), nil

	case StrategyAPIKey:
		if c.APIKey == nil {
			return nil, &AuthError{
				Code:             "AUTH_RESOLVER_NOT_CONFIGURED",
				Strategy:         "api_key",
				HumanRemediation: "api_key resolver not wired on this dispatcher",
			}
		}
		creds, err := c.APIKey.Resolve(ctx, scopeNames)
		if err != nil {
			return nil, err
		}
		return creds.ToDispatchCredentials(), nil

	case StrategyGUMOAuth:
		// The resolver runs the manifest gate first; when no scope is yet
		// promoted to (active, verified, ready, passing) the gate returns
		// GUM_OAUTH_MANAGED_CLIENT_NOT_READY — semantically equivalent to
		// the prior AUTH_STRATEGY_DISABLED branch but for the precise
		// reason rather than a blanket disable.
		if c.GumOAuth == nil {
			c.GumOAuth = NewGumOAuth()
		}
		creds, err := c.GumOAuth.Resolve(ctx, scopeNames)
		if err != nil {
			return nil, err
		}
		return creds.ToDispatchCredentials(), nil

	case StrategyNone:
		// auth_strategy=none means the op needs NO upstream credential
		// resolution. The gum.code meta-op uses it: the Risor sandbox
		// re-dispatches any catalog ops it calls (via gum_call) through a
		// fresh dispatch cycle that resolves THAT op's own auth_strategy, so
		// the meta-op itself carries no credential. Returning (nil, nil) lets
		// the dispatcher proceed to the adapter with no Authorization header.
		return nil, nil

	case StrategyPluginManaged:
		// auth_strategy=plugin_managed means the plugin owns its own credential
		// descriptors and rate-limit budget; the gum host does NOT participate in
		// the plugin's authentication (spec §1.1, gen_unofficial_plugins.go). Like
		// StrategyNone, return (nil, nil) so dispatch proceeds to the plugin-mcp
		// adapter — which executes the bundled plugin or, when it isn't installed,
		// surfaces the documented SERVICE_DOWN+adapter_key "plugin not installed"
		// shape. Previously this fell through to AUTH_STRATEGY_NOT_IMPLEMENTED,
		// making flights/scholar/patents/youtube/trends dead on discovery.
		return nil, nil

	case StrategyCompound:
		// Spec §7 lines 1289-1305 + 1378-1389: a compound-auth failure
		// envelope MUST include auth_strategy, missing_components, and
		// setup_command so the LLM/user can act on it. v0.1.0 does not
		// have a per-component resolver chain yet (gum-qa3 carries the
		// scaffold), so the missing_components slice reflects the
		// variant's declared components when present and falls back to
		// a single "see_setup_command" marker otherwise. The op id is
		// surfaced when the dispatcher has resolved it so the operator
		// can run `gum auth setup <op_id>` verbatim.
		missing := compoundMissingComponents(rv)
		opID := ""
		if inv != nil {
			opID = inv.OpID
		}
		setup := "gum auth setup"
		if opID != "" {
			setup = "gum auth setup " + opID
		}
		return nil, &AuthError{
			Code:              "AUTH_REQUIRED",
			Strategy:          "compound",
			HumanRemediation:  "compound auth requires multiple components; run " + setup + " to walk each missing prerequisite",
			MissingComponents: missing,
			SetupCommand:      setup,
			OpID:              opID,
			UserMessage:       "This operation needs additional credentials. Run: " + setup,
			Retryable:         false,
		}

	default:
		return nil, &AuthError{
			Code:             "AUTH_STRATEGY_NOT_IMPLEMENTED",
			Strategy:         strat.String(),
			HumanRemediation: fmt.Sprintf("strategy %q is not wired in v0.1.0", strat.String()),
		}
	}
}

// compoundMissingComponents derives the missing_components slice for a
// compound-auth failure envelope. v0.1.0 does not yet carry per-variant
// component records in catalog.json (compound auth lands fully with
// plugin-managed manifests post-v0.1), so the fallback is a single
// "see_setup_command" sentinel — enough to satisfy spec §7's "MUST
// include missing_components" while signaling that the canonical
// component list lives behind `gum auth setup <op_id>`.
func compoundMissingComponents(rv *dispatch.ResolvedVariant) []string {
	if rv == nil || rv.Variant == nil {
		return []string{"see_setup_command"}
	}
	// Future: pull from rv.Variant.MissingComponents once the catalog
	// ABI carries the closed-enum component list (spec §7 line 1296).
	return []string{"see_setup_command"}
}
