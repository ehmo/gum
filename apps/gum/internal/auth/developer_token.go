package auth

import (
	"os"
	"strings"
)

// EnvGoogleAdsDeveloperToken is the env fallback the Google Ads adapter reads
// for the developer token. The per-profile keychain entry
// (gum.google_ads.developer_token.<profile>) is the default; the env var stays
// as a CI/automation and MCP-server-config fallback. Either way the developer
// token is sourced server-side and NEVER travels as an invocation arg, so it
// does not leak into the audit log, args_canonical, the cache key, or the MCP
// tool-call context.
const EnvGoogleAdsDeveloperToken = "GUM_GOOGLE_ADS_DEVELOPER_TOKEN"

// developerTokenKeyringKey returns the canonical OS-keychain key for the Google
// Ads developer token, scoped per profile so dev/prod profiles don't collide.
func developerTokenKeyringKey(profile string) string {
	p := strings.TrimSpace(profile)
	if p == "" {
		p = DefaultAPIKeyProfile
	}
	return "gum.google_ads.developer_token." + p
}

// StoreDeveloperToken persists tok under the per-profile keyring entry. Returns
// AUTH_KEYCHAIN_UNAVAILABLE wrapped in *AuthError when no backend is configured.
func StoreDeveloperToken(kb KeyringBackend, profile, tok string) error {
	if kb == nil {
		return &AuthError{
			Code:             "AUTH_KEYCHAIN_UNAVAILABLE",
			Strategy:         "compound",
			HumanRemediation: "no keyring backend configured; set " + EnvGoogleAdsDeveloperToken + " instead",
		}
	}
	return kb.Set(developerTokenKeyringKey(profile), strings.TrimSpace(tok))
}

// LookupDeveloperToken returns the persisted developer token for profile,
// preferring the OS keychain and falling back to the env var. Returns "" when
// neither is set. Keyring backend errors (AUTH_KEYCHAIN_UNAVAILABLE on an
// air-gapped host) fall through to the env var so CI without a keychain works.
func LookupDeveloperToken(kb KeyringBackend, profile string) string {
	if kb != nil {
		if v, _ := kb.Get(developerTokenKeyringKey(profile)); strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(os.Getenv(EnvGoogleAdsDeveloperToken))
}

// DeleteDeveloperToken removes the persisted developer token for profile.
// Absent keys are not an error.
func DeleteDeveloperToken(kb KeyringBackend, profile string) error {
	if kb == nil {
		return nil
	}
	return kb.Delete(developerTokenKeyringKey(profile))
}
