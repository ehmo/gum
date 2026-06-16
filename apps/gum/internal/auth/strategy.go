// Package auth implements the closed-enum auth strategies (spec.md §7, §14).
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
)

// Strategy is the closed enum of auth strategies. Integer values map 1:1 to the
// catalog.AuthStrategy string constants. Only byo_oauth (1) and adc (2) are implemented
// in v0.1.0; all others return AUTH_STRATEGY_NOT_IMPLEMENTED without panicking.
type Strategy int

const (
	// StrategyGUMOAuth corresponds to catalog.AuthStrategyGUMOAuth ("gum_oauth").
	// Implemented via NewGumOAuth() (PKCE + loopback + CSRF state); the
	// docs/auth-managed-scopes.v1.json manifest gates start-up so the
	// strategy is effectively disabled until a scope reaches the
	// (active, verified, ready, passing) state.
	StrategyGUMOAuth Strategy = iota
	// StrategyBYOOAuth corresponds to catalog.AuthStrategyBYOOAuth ("byo_oauth"). Implemented.
	StrategyBYOOAuth
	// StrategyADC corresponds to catalog.AuthStrategyADC ("adc"). Implemented.
	StrategyADC
	// StrategyAPIKey corresponds to catalog.AuthStrategyAPIKey ("api_key"). Stubbed.
	StrategyAPIKey
	// StrategyServiceAccountKey corresponds to catalog.AuthStrategyServiceAccountKey
	// ("service_account_key"). Stubbed.
	StrategyServiceAccountKey
	// StrategyWorkloadIdentity corresponds to catalog.AuthStrategyWorkloadIdentity
	// ("workload_identity"). Stubbed.
	StrategyWorkloadIdentity
	// StrategyImpersonation corresponds to catalog.AuthStrategyImpersonation
	// ("impersonation"). Stubbed.
	StrategyImpersonation
	// StrategyNone corresponds to catalog.AuthStrategyNone ("none"). Stubbed.
	StrategyNone
	// StrategyCompound corresponds to catalog.AuthStrategyCompound ("compound"). Stubbed.
	StrategyCompound
	// StrategyPluginManaged corresponds to catalog.AuthStrategyPluginManaged ("plugin_managed"). Stubbed.
	StrategyPluginManaged
)

// Sentinel errors.
var (
	// ErrUnknownStrategy is returned by Resolve when the catalog variant's
	// auth_strategy value does not match any of the 8 known strategies.
	ErrUnknownStrategy = errors.New("auth: unknown strategy")

	// ErrAuthStrategyNotImplemented is returned by Acquire when the strategy
	// is known but not yet wired (i.e. everything except byo_oauth and adc).
	ErrAuthStrategyNotImplemented = errors.New("auth: strategy not implemented in v0.1.0")
)

// AuthError is a structured auth failure with machine-readable Code and a
// human-readable remediation hint shown to the user. The optional envelope
// fields (MissingComponents, SetupCommand, OpID, RequiredScopes, HaveScopes,
// UserMessage, Retryable) carry the spec §7 lines 1289-1305 / 1378-1389
// compound + scope-missing payload that MUST accompany any non-gum_oauth
// auth failure so the host can guide the user to the right setup command.
type AuthError struct {
	// Code is a short uppercase token e.g. "TOKEN_EXPIRED", "NO_ADC_CREDENTIALS".
	Code string
	// Strategy is the canonical catalog strategy name e.g. "adc".
	Strategy string
	// HumanRemediation is a plain-English action the user can take to fix the issue.
	HumanRemediation string
	// MissingComponents is the list of standardized component kinds the
	// variant declared but the resolver could not satisfy (spec §7
	// lines 1294-1305). Required on compound-auth envelopes.
	MissingComponents []string
	// SetupCommand is the canonical CLI to run next (e.g.
	// "gum auth setup <op_id>"). Required on compound-auth envelopes.
	SetupCommand string
	// OpID is the catalog op identifier the request targeted, when known.
	OpID string
	// RequiredScopes / HaveScopes carry the SCOPE_MISSING payload for
	// gum_oauth and byo_oauth strategies (spec §7 line 1380-1389).
	RequiredScopes []string
	HaveScopes     []string
	// UserMessage is a one-sentence, user-facing summary. When empty the
	// host falls back to HumanRemediation.
	UserMessage string
	// Retryable signals whether the LLM should retry the same call once
	// the user finishes the setup_command (spec §7 line 1388). Defaults to
	// false; compound and scope-missing failures should set this true once
	// the missing components are resolved.
	Retryable bool
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("auth [%s/%s]: %s", e.Strategy, e.Code, e.HumanRemediation)
}

// MarshalJSON emits the canonical spec §7 (lines 1378-1389) envelope shape
// so MCP stdio mode and the CLI can forward the structured error to the
// caller without translation. Empty optional fields are omitted.
func (e *AuthError) MarshalJSON() ([]byte, error) {
	type envelope struct {
		ErrorCode         string   `json:"error_code"`
		AuthStrategy      string   `json:"auth_strategy,omitempty"`
		OpID              string   `json:"op_id,omitempty"`
		MissingComponents []string `json:"missing_components,omitempty"`
		RequiredScopes    []string `json:"required_scopes,omitempty"`
		HaveScopes        []string `json:"have_scopes,omitempty"`
		SetupCommand      string   `json:"setup_command,omitempty"`
		UserMessage       string   `json:"user_message,omitempty"`
		Retryable         bool     `json:"retryable,omitempty"`
	}
	user := e.UserMessage
	if user == "" {
		user = e.HumanRemediation
	}
	return json.Marshal(envelope{
		ErrorCode:         e.Code,
		AuthStrategy:      e.Strategy,
		OpID:              e.OpID,
		MissingComponents: e.MissingComponents,
		RequiredScopes:    e.RequiredScopes,
		HaveScopes:        e.HaveScopes,
		SetupCommand:      e.SetupCommand,
		UserMessage:       user,
		Retryable:         e.Retryable,
	})
}

// Is satisfies errors.Is for sentinel matching. Currently handles
// ErrAuthStrategyNotImplemented so callers can use errors.Is(err, ErrAuthStrategyNotImplemented).
func (e *AuthError) Is(target error) bool {
	if target == ErrAuthStrategyNotImplemented && e.Code == "AUTH_STRATEGY_NOT_IMPLEMENTED" {
		return true
	}
	return false
}

// Credentials carries the resolved auth material produced by Acquire.
// It intentionally mirrors dispatch.Credentials but carries richer metadata
// for logging and cache-key derivation.
type Credentials struct {
	// Token is the Bearer token value (access token).
	Token string
	// APIKey is the raw Google API key value used for auth_strategy=api_key
	// variants. Mutually exclusive with Token at the wire boundary: only one
	// of {Token, APIKey} is sent on a given request.
	APIKey string
	// ExpiresAt is the UTC time at which Token expires. Zero value = unknown expiry.
	ExpiresAt time.Time
	// Scopes is the list of OAuth scopes this token covers.
	Scopes []string
	// StrategyName is the canonical catalog name of the strategy that produced this token.
	StrategyName string
	// QuotaProjectID is the GCP project to attribute quota/billing to. Sent as
	// the X-Goog-User-Project header. Required by some Google APIs (e.g.
	// Search Console) when authenticating with user ADC. May be empty.
	QuotaProjectID string
	// SubjectFingerprint is the stable per-principal opaque ID derived by the
	// resolver from credential material that uniquely identifies the subject
	// (e.g. SHA-256 of the refresh token for byo_oauth, SHA-256 of the ADC
	// JSON for adc). Spec §10.0.1: this scopes the semantic cache, tee
	// artifacts, gain ledger, and audit so credential switching never replays
	// the prior subject's data.
	SubjectFingerprint string
}

// ToDispatchCredentials converts auth.Credentials to the dispatch-layer wire type.
func (c *Credentials) ToDispatchCredentials() *dispatch.Credentials {
	return &dispatch.Credentials{
		Token:              c.Token,
		APIKey:             c.APIKey,
		QuotaProjectID:     c.QuotaProjectID,
		SubjectFingerprint: c.SubjectFingerprint,
	}
}

// String returns the canonical catalog strategy name (e.g. "byo_oauth").
func (s Strategy) String() string {
	switch s {
	case StrategyGUMOAuth:
		return "gum_oauth"
	case StrategyBYOOAuth:
		return "byo_oauth"
	case StrategyADC:
		return "adc"
	case StrategyAPIKey:
		return "api_key"
	case StrategyServiceAccountKey:
		return "service_account_key"
	case StrategyWorkloadIdentity:
		return "workload_identity"
	case StrategyImpersonation:
		return "impersonation"
	case StrategyNone:
		return "none"
	case StrategyCompound:
		return "compound"
	case StrategyPluginManaged:
		return "plugin_managed"
	default:
		return "unknown"
	}
}

// strategyFromCatalog converts a catalog.AuthStrategy string to the Strategy enum.
// Returns (0, ErrUnknownStrategy) for any unrecognised string.
func strategyFromCatalog(as catalog.AuthStrategy) (Strategy, error) {
	switch as {
	case catalog.AuthStrategyGUMOAuth:
		return StrategyGUMOAuth, nil
	case catalog.AuthStrategyBYOOAuth:
		return StrategyBYOOAuth, nil
	case catalog.AuthStrategyADC:
		return StrategyADC, nil
	case catalog.AuthStrategyAPIKey:
		return StrategyAPIKey, nil
	case catalog.AuthStrategyServiceAccountKey, catalog.AuthStrategyServiceAccount:
		// "service_account" is the catalog-abi alias for "service_account_key"
		// (both accepted by Validate). Map both to the same resolver, else a
		// variant using the alias passes validation but fails AUTH_REQUIRED.
		return StrategyServiceAccountKey, nil
	case catalog.AuthStrategyWorkloadIdentity:
		return StrategyWorkloadIdentity, nil
	case catalog.AuthStrategyImpersonation:
		return StrategyImpersonation, nil
	case catalog.AuthStrategyNone:
		return StrategyNone, nil
	case catalog.AuthStrategyCompound:
		return StrategyCompound, nil
	case catalog.AuthStrategyPluginManaged:
		return StrategyPluginManaged, nil
	default:
		return 0, ErrUnknownStrategy
	}
}

// Resolver is the interface both ByoOAuth and ADCResolver satisfy so the
// dispatcher can call them polymorphically.
type Resolver interface {
	Resolve(ctx context.Context, scopes []string) (*Credentials, error)
}

// Resolve maps the variant's auth_strategy to a Strategy enum.
// Returns ErrUnknownStrategy for any unrecognised value.
func Resolve(ctx context.Context, variant *catalog.Variant) (Strategy, error) {
	return strategyFromCatalog(variant.AuthStrategy)
}

// Acquire obtains live credentials for strat using the ambient environment.
// Returns AUTH_STRATEGY_NOT_IMPLEMENTED for the 6 stubbed strategies.
func Acquire(ctx context.Context, strat Strategy, scopes []string) (*Credentials, error) {
	switch strat {
	case StrategyBYOOAuth:
		return nil, &AuthError{
			Code:             "AUTH_ACQUIRE_REQUIRES_INSTANCE",
			Strategy:         strat.String(),
			HumanRemediation: "use NewByoOAuth(...) or NewADCResolver() and call .Acquire() / .Resolve() directly",
		}
	case StrategyADC:
		return nil, &AuthError{
			Code:             "AUTH_ACQUIRE_REQUIRES_INSTANCE",
			Strategy:         strat.String(),
			HumanRemediation: "use NewByoOAuth(...) or NewADCResolver() and call .Acquire() / .Resolve() directly",
		}
	case StrategyAPIKey:
		return nil, &AuthError{
			Code:             "AUTH_ACQUIRE_REQUIRES_INSTANCE",
			Strategy:         strat.String(),
			HumanRemediation: "use NewAPIKeyResolver() and call .Resolve() directly; api_key reads GUM_API_KEY in v0.1.0",
		}
	case StrategyServiceAccountKey:
		return nil, &AuthError{
			Code:             "AUTH_ACQUIRE_REQUIRES_INSTANCE",
			Strategy:         strat.String(),
			HumanRemediation: "use NewServiceAccountResolver(path) and call .Resolve() directly; service_account reads GUM_SERVICE_ACCOUNT_KEY for the JSON path in v0.1.0",
		}
	case StrategyGUMOAuth:
		return nil, &AuthError{
			Code:             "AUTH_ACQUIRE_REQUIRES_INSTANCE",
			Strategy:         strat.String(),
			HumanRemediation: "use NewGumOAuth() and call .Login() / .Resolve() directly; gum_oauth is gated by the managed-scopes manifest (docs/auth-managed-scopes.v1.json)",
		}
	case StrategyWorkloadIdentity, StrategyImpersonation, StrategyNone, StrategyCompound, StrategyPluginManaged:
		return nil, &AuthError{
			Code:             "AUTH_STRATEGY_NOT_IMPLEMENTED",
			Strategy:         strat.String(),
			HumanRemediation: "this strategy is not implemented in v0.1.0; see spec.md §7",
		}
	default:
		return nil, ErrUnknownStrategy
	}
}
