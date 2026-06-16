package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// EnvAPIKeyVar is the env variable the v0.1.0 api_key resolver reads. Spec
// §7 line 1284 mandates keychain storage; the keychain path is the default,
// the env var stays as a CI/automation fallback. See docs/known-divergences.md
// and bd memo gum-auth-strategy-v3 for the migration history.
const EnvAPIKeyVar = "GUM_API_KEY"

// DefaultAPIKeyProfile is the profile name used when the caller does not
// scope an API key to a specific profile.
const DefaultAPIKeyProfile = "default"

// apiKeyKeyringKey returns the canonical OS-keychain key for the api_key
// strategy. Per-profile so distinct profiles (dev/prod) don't collide.
func apiKeyKeyringKey(profile string) string {
	p := strings.TrimSpace(profile)
	if p == "" {
		p = DefaultAPIKeyProfile
	}
	return fmt.Sprintf("gum.api_key.%s", p)
}

// StoreAPIKey persists key under the per-profile keyring entry. Returns
// AUTH_KEYCHAIN_UNAVAILABLE wrapped in *AuthError when the backend is
// unsupported on this platform.
func StoreAPIKey(kb KeyringBackend, profile, key string) error {
	if kb == nil {
		return &AuthError{
			Code:             "AUTH_KEYCHAIN_UNAVAILABLE",
			Strategy:         "api_key",
			HumanRemediation: "no keyring backend configured",
		}
	}
	return kb.Set(apiKeyKeyringKey(profile), key)
}

// LookupAPIKey returns the persisted api_key for profile, or "" when absent.
// Errors propagate the keyring backend's error envelope.
func LookupAPIKey(kb KeyringBackend, profile string) (string, error) {
	if kb == nil {
		return "", nil
	}
	return kb.Get(apiKeyKeyringKey(profile))
}

// DeleteAPIKey removes the persisted api_key for profile. Absent keys are
// not an error.
func DeleteAPIKey(kb KeyringBackend, profile string) error {
	if kb == nil {
		return nil
	}
	return kb.Delete(apiKeyKeyringKey(profile))
}

// APIKeyResolver resolves api_key credentials from the operator's
// environment. It satisfies the Resolver interface so CompositeResolver
// can dispatch to it the same way it dispatches to ADC.
//
// v0.1.0 storage is the GUM_API_KEY env variable. Future revisions move
// the read to the per-profile keychain entry (gum-0wv); the resolver
// interface stays stable so the wiring in CompositeResolver does not
// change when storage moves.
type APIKeyResolver struct {
	// Lookup returns the API key for the active profile. Defaults to
	// (1) os-keyring lookup → (2) GUM_API_KEY env var. Tests inject a
	// fixed value.
	Lookup func() string

	// Keyring backs the keyring lookup branch. nil means env-var only
	// (CI/automation default). The CLI sets this to NewOSKeyring() and
	// tests can inject a keyring.MockInit()-backed value.
	Keyring KeyringBackend
	// Profile scopes the keyring entry (gum.api_key.<profile>).
	Profile string
}

// NewAPIKeyResolver returns a resolver that prefers the OS keychain entry
// for the active profile and falls back to the GUM_API_KEY env var.
func NewAPIKeyResolver() *APIKeyResolver {
	r := &APIKeyResolver{
		Keyring: NewOSKeyring(),
		Profile: DefaultAPIKeyProfile,
	}
	r.Lookup = r.defaultLookup
	return r
}

// defaultLookup returns the keyring value when present, otherwise the env
// var. Keyring errors (AUTH_KEYCHAIN_UNAVAILABLE) silently fall through to
// the env-var branch so air-gapped CI without a backend still works.
func (r *APIKeyResolver) defaultLookup() string {
	if r.Keyring != nil {
		if v, _ := r.Keyring.Get(apiKeyKeyringKey(r.Profile)); strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(os.Getenv(EnvAPIKeyVar))
}

// Resolve returns Credentials with APIKey set and Token empty. Scopes are
// preserved for audit-trail symmetry with OAuth strategies even though
// API keys are not scoped at acquisition time — the upstream API enforces
// any IAM restriction attached to the key itself.
//
// Returns AUTH_API_KEY_MISSING when the env var is unset so the operator
// sees an actionable error instead of a silent 401 from Google's edge.
func (r *APIKeyResolver) Resolve(_ context.Context, scopes []string) (*Credentials, error) {
	key := ""
	if r != nil && r.Lookup != nil {
		key = r.Lookup()
	}
	if key == "" {
		return nil, &AuthError{
			Code:             "AUTH_API_KEY_MISSING",
			Strategy:         "api_key",
			HumanRemediation: "run `gum auth use-api-key --stdin` (keychain) or set the GUM_API_KEY env variable",
		}
	}
	return &Credentials{
		APIKey:             key,
		Scopes:             append([]string{}, scopes...),
		StrategyName:       "api_key",
		SubjectFingerprint: apiKeyFingerprint(key),
	}, nil
}

// apiKeyFingerprint returns the spec §10.0.1-compatible double-hashed
// subject fingerprint described in spec.md line 2217: sha256 of
// sha256(raw_api_key). The double-hash ensures the audit log never
// stores a value that could collide with a raw token search.
func apiKeyFingerprint(key string) string {
	first := sha256.Sum256([]byte(key))
	hexFirst := hex.EncodeToString(first[:])
	second := sha256.Sum256([]byte(hexFirst))
	return hex.EncodeToString(second[:])
}
