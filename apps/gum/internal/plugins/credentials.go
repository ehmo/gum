// Spec §1606: credential descriptor types, validation, and keychain key helpers.
// User-facing surfaces (errors, resource records, prompts) MUST use alias,
// display_name, and setup_hint only; raw env var names MUST NOT appear in any
// message returned to the user (spec §1414, §1606).

package plugins

import (
	"errors"
	"fmt"
	"regexp"
)

// ErrCredentialDescriptorInvalid is returned by ValidateCredentialDescriptors
// when the manifest's credential_descriptors block violates spec §1606 rules.
// The error wraps a detail message but the sentinel itself is stable.
var ErrCredentialDescriptorInvalid = errors.New("PLUGIN_CREDENTIAL_DESCRIPTOR_INVALID")

// validCredentialKinds is the closed enum of credential kinds per spec §1606.
var validCredentialKinds = map[string]bool{
	"api_key":     true,
	"oauth_token": true,
	"cookie":      true,
	"session":     true,
	"other":       true,
}

// credAliasRe is the pattern for a valid descriptor alias: lowercase token
// per spec §1606 example "flights_session".
var credAliasRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// CredentialDescriptor carries the user-facing metadata for one plugin
// credential per spec §1606. The Env field is the raw subprocess env var name
// and MUST NOT appear in any user-visible output.
type CredentialDescriptor struct {
	// Alias is the stable lowercase token used in user-facing error messages
	// and keychain keys (e.g. "flights_session"). Never the raw env var name.
	Alias string `json:"alias"`
	// Env is the raw subprocess env var name (e.g. "GUM_FLIGHTS_SESSION").
	// MUST NOT be exposed to the user in any message.
	Env string `json:"env"`
	// Kind is one of: api_key, oauth_token, cookie, session, other.
	Kind string `json:"kind"`
	// DisplayName is the human-readable credential name shown in prompts (max 80 chars).
	DisplayName string `json:"display_name"`
	// SetupHint is a short instruction for obtaining the credential (max 160 chars, no secrets).
	SetupHint string `json:"setup_hint"`
}

// ValidateCredentialDescriptors enforces the spec §1606 invariants on the
// manifest's needs_user_creds / credential_descriptors pair:
//
//   - Every env name in needs_user_creds has exactly one descriptor.
//   - No descriptor exists without a matching needs_user_creds entry.
//   - alias matches [a-z][a-z0-9_]{0,63}.
//   - env is non-empty.
//   - kind is in the closed enum.
//   - display_name ≤ 80 chars, non-empty.
//   - setup_hint ≤ 160 chars (may be empty).
//   - No duplicate aliases.
//
// Returns a wrapped ErrCredentialDescriptorInvalid on the first violation.
func ValidateCredentialDescriptors(needs []string, descs []CredentialDescriptor) error {
	if len(needs) == 0 && len(descs) == 0 {
		return nil
	}

	// Build a set of env names required.
	needsSet := make(map[string]bool, len(needs))
	for _, env := range needs {
		needsSet[env] = true
	}

	// Build an env → descriptor map and validate each descriptor.
	byEnv := make(map[string]CredentialDescriptor, len(descs))
	seenAlias := make(map[string]bool, len(descs))
	for _, d := range descs {
		if d.Env == "" {
			return fmt.Errorf("%w: descriptor has empty env field", ErrCredentialDescriptorInvalid)
		}
		if !needsSet[d.Env] {
			// Descriptor references an env not in needs_user_creds.
			// Do NOT include the env var name in the message returned to users
			// (the validation error is a manifest-author error shown in CLI
			// diagnostics only — but we keep the message env-free to satisfy
			// spec §1606 "may appear only in local CLI diagnostics and manifest
			// validation errors").
			return fmt.Errorf("%w: descriptor env not listed in needs_user_creds", ErrCredentialDescriptorInvalid)
		}
		if _, dup := byEnv[d.Env]; dup {
			return fmt.Errorf("%w: duplicate descriptor for env entry", ErrCredentialDescriptorInvalid)
		}
		byEnv[d.Env] = d

		if !credAliasRe.MatchString(d.Alias) {
			return fmt.Errorf("%w: alias %q does not match [a-z][a-z0-9_]{0,63}", ErrCredentialDescriptorInvalid, d.Alias)
		}
		if seenAlias[d.Alias] {
			return fmt.Errorf("%w: duplicate alias %q", ErrCredentialDescriptorInvalid, d.Alias)
		}
		seenAlias[d.Alias] = true

		if !validCredentialKinds[d.Kind] {
			return fmt.Errorf("%w: alias %q has invalid kind %q (must be api_key|oauth_token|cookie|session|other)",
				ErrCredentialDescriptorInvalid, d.Alias, d.Kind)
		}
		if d.DisplayName == "" {
			return fmt.Errorf("%w: alias %q has empty display_name", ErrCredentialDescriptorInvalid, d.Alias)
		}
		if len(d.DisplayName) > 80 {
			return fmt.Errorf("%w: alias %q display_name exceeds 80 chars", ErrCredentialDescriptorInvalid, d.Alias)
		}
		if len(d.SetupHint) > 160 {
			return fmt.Errorf("%w: alias %q setup_hint exceeds 160 chars", ErrCredentialDescriptorInvalid, d.Alias)
		}
	}

	// Every needs_user_creds entry must have a descriptor.
	for _, env := range needs {
		if _, ok := byEnv[env]; !ok {
			return fmt.Errorf("%w: missing descriptor for needs_user_creds entry", ErrCredentialDescriptorInvalid)
		}
	}

	return nil
}

// PluginCredentialKey returns the keychain key used to store a plugin
// credential for (profile, pluginID, alias). The key format is:
//
//	gum.plugin.<profile>.<pluginID>.<alias>
//
// This never embeds the raw env var name so the keychain is safe to audit.
func PluginCredentialKey(profile, pluginID, alias string) string {
	return fmt.Sprintf("gum.plugin.%s.%s.%s", profile, pluginID, alias)
}

// SafeDescriptorMaps returns a slice of maps containing only the
// user-safe fields (alias, kind, display_name, setup_hint) for each descriptor.
// This is the shape persisted to plugin-state.json and surfaced in MCP resources
// per spec §3165.
func SafeDescriptorMaps(descs []CredentialDescriptor) []any {
	if len(descs) == 0 {
		return nil
	}
	out := make([]any, 0, len(descs))
	for _, d := range descs {
		out = append(out, map[string]any{
			"alias":        d.Alias,
			"kind":         d.Kind,
			"display_name": d.DisplayName,
			"setup_hint":   d.SetupHint,
		})
	}
	return out
}
