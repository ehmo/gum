// Acceptance tests for the spec §1606 credential descriptor types and
// validation rules. These are the normative contract tests for
// ValidateCredentialDescriptors and PluginCredentialKey.

package plugins_test

import (
	"errors"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/plugins"
)

// TestValidateCredentialDescriptorsHappyPath verifies that a well-formed set of
// descriptors passes validation without error.
func TestValidateCredentialDescriptorsHappyPath(t *testing.T) {
	defer goleak.VerifyNone(t)

	needs := []string{"GUM_API_TOKEN", "GUM_REFRESH"}
	descs := []plugins.CredentialDescriptor{
		{
			Alias:       "api_token",
			Env:         "GUM_API_TOKEN",
			Kind:        "api_key",
			DisplayName: "API Token",
			SetupHint:   "Generate at https://example.com/token",
		},
		{
			Alias:       "refresh_tok",
			Env:         "GUM_REFRESH",
			Kind:        "oauth_token",
			DisplayName: "OAuth Refresh Token",
			SetupHint:   "Obtain via OAuth flow",
		},
	}

	if err := plugins.ValidateCredentialDescriptors(needs, descs); err != nil {
		t.Errorf("ValidateCredentialDescriptors returned unexpected error: %v", err)
	}
}

// TestValidateCredentialDescriptorsBothEmpty verifies that empty needs + empty
// descriptors is valid (plugin has no credential requirements).
func TestValidateCredentialDescriptorsBothEmpty(t *testing.T) {
	defer goleak.VerifyNone(t)

	if err := plugins.ValidateCredentialDescriptors(nil, nil); err != nil {
		t.Errorf("ValidateCredentialDescriptors(nil, nil) = %v; want nil", err)
	}
}

// TestValidateCredentialDescriptorsMissingDescriptor verifies that a
// needs_user_creds entry without a matching descriptor fails with
// ErrCredentialDescriptorInvalid.
func TestValidateCredentialDescriptorsMissingDescriptor(t *testing.T) {
	defer goleak.VerifyNone(t)

	needs := []string{"GUM_MISSING"}
	descs := []plugins.CredentialDescriptor{} // no descriptor

	err := plugins.ValidateCredentialDescriptors(needs, descs)
	if !errors.Is(err, plugins.ErrCredentialDescriptorInvalid) {
		t.Errorf("expected ErrCredentialDescriptorInvalid, got: %v", err)
	}
}

// TestValidateCredentialDescriptorsExtraDescriptor verifies that a descriptor
// whose env is not in needs_user_creds fails validation.
func TestValidateCredentialDescriptorsExtraDescriptor(t *testing.T) {
	defer goleak.VerifyNone(t)

	needs := []string{} // empty — no env required
	descs := []plugins.CredentialDescriptor{
		{
			Alias:       "rogue",
			Env:         "GUM_ROGUE",
			Kind:        "api_key",
			DisplayName: "Rogue Credential",
		},
	}

	err := plugins.ValidateCredentialDescriptors(needs, descs)
	if !errors.Is(err, plugins.ErrCredentialDescriptorInvalid) {
		t.Errorf("expected ErrCredentialDescriptorInvalid for extra descriptor, got: %v", err)
	}
}

// TestValidateCredentialDescriptorsDuplicateAlias verifies that two descriptors
// with the same alias fail validation.
func TestValidateCredentialDescriptorsDuplicateAlias(t *testing.T) {
	defer goleak.VerifyNone(t)

	needs := []string{"GUM_A", "GUM_B"}
	descs := []plugins.CredentialDescriptor{
		{Alias: "same_alias", Env: "GUM_A", Kind: "api_key", DisplayName: "A"},
		{Alias: "same_alias", Env: "GUM_B", Kind: "api_key", DisplayName: "B"},
	}

	err := plugins.ValidateCredentialDescriptors(needs, descs)
	if !errors.Is(err, plugins.ErrCredentialDescriptorInvalid) {
		t.Errorf("expected ErrCredentialDescriptorInvalid for duplicate alias, got: %v", err)
	}
}

// TestValidateCredentialDescriptorsInvalidKind verifies that an unknown kind
// value fails validation.
func TestValidateCredentialDescriptorsInvalidKind(t *testing.T) {
	defer goleak.VerifyNone(t)

	needs := []string{"GUM_X"}
	descs := []plugins.CredentialDescriptor{
		{Alias: "x", Env: "GUM_X", Kind: "bearer_token", DisplayName: "X"},
	}

	err := plugins.ValidateCredentialDescriptors(needs, descs)
	if !errors.Is(err, plugins.ErrCredentialDescriptorInvalid) {
		t.Errorf("expected ErrCredentialDescriptorInvalid for invalid kind, got: %v", err)
	}
}

// TestValidateCredentialDescriptorsDisplayNameTooLong verifies that a
// display_name exceeding 80 characters fails validation.
func TestValidateCredentialDescriptorsDisplayNameTooLong(t *testing.T) {
	defer goleak.VerifyNone(t)

	longName := string(make([]byte, 81))
	for i := range longName {
		longName = longName[:i] + "x" + longName[i+1:]
	}

	needs := []string{"GUM_Y"}
	descs := []plugins.CredentialDescriptor{
		{Alias: "y", Env: "GUM_Y", Kind: "api_key", DisplayName: longName},
	}

	err := plugins.ValidateCredentialDescriptors(needs, descs)
	if !errors.Is(err, plugins.ErrCredentialDescriptorInvalid) {
		t.Errorf("expected ErrCredentialDescriptorInvalid for long display_name, got: %v", err)
	}
}

// TestValidateCredentialDescriptorsSetupHintTooLong verifies that a setup_hint
// exceeding 160 characters fails validation.
func TestValidateCredentialDescriptorsSetupHintTooLong(t *testing.T) {
	defer goleak.VerifyNone(t)

	longHint := string(make([]byte, 161))
	for i := range longHint {
		longHint = longHint[:i] + "h" + longHint[i+1:]
	}

	needs := []string{"GUM_Z"}
	descs := []plugins.CredentialDescriptor{
		{Alias: "z", Env: "GUM_Z", Kind: "session", DisplayName: "Z", SetupHint: longHint},
	}

	err := plugins.ValidateCredentialDescriptors(needs, descs)
	if !errors.Is(err, plugins.ErrCredentialDescriptorInvalid) {
		t.Errorf("expected ErrCredentialDescriptorInvalid for long setup_hint, got: %v", err)
	}
}

// TestValidateCredentialDescriptorsAllKinds verifies that all five valid kind
// values are accepted.
func TestValidateCredentialDescriptorsAllKinds(t *testing.T) {
	defer goleak.VerifyNone(t)

	kinds := []string{"api_key", "oauth_token", "cookie", "session", "other"}
	for _, kind := range kinds {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			needs := []string{"GUM_K"}
			descs := []plugins.CredentialDescriptor{
				{Alias: "k", Env: "GUM_K", Kind: kind, DisplayName: "K"},
			}
			if err := plugins.ValidateCredentialDescriptors(needs, descs); err != nil {
				t.Errorf("kind=%q: unexpected error: %v", kind, err)
			}
		})
	}
}

// TestPluginCredentialKey verifies the keychain key format never includes
// raw env var names and follows the expected pattern.
func TestPluginCredentialKey(t *testing.T) {
	defer goleak.VerifyNone(t)

	key := plugins.PluginCredentialKey("default", "my-plugin", "api_token")
	want := "gum.plugin.default.my-plugin.api_token"
	if key != want {
		t.Errorf("PluginCredentialKey = %q; want %q", key, want)
	}

	// Assert the key does NOT contain any uppercase (env vars are uppercase).
	for _, c := range key {
		if c >= 'A' && c <= 'Z' {
			t.Errorf("PluginCredentialKey contains uppercase char %q (possible env var leak): %q", c, key)
		}
	}
}

// TestSafeDescriptorMapsOmitsEnv verifies that SafeDescriptorMaps never
// includes the raw env var name in the returned maps.
func TestSafeDescriptorMapsOmitsEnv(t *testing.T) {
	defer goleak.VerifyNone(t)

	descs := []plugins.CredentialDescriptor{
		{
			Alias:       "flights_session",
			Env:         "GUM_FLIGHTS_SESSION",
			Kind:        "session",
			DisplayName: "Flights Session",
			SetupHint:   "Log into Google Flights and extract the session cookie",
		},
	}

	maps := plugins.SafeDescriptorMaps(descs)
	if len(maps) != 1 {
		t.Fatalf("SafeDescriptorMaps len = %d; want 1", len(maps))
	}
	m, ok := maps[0].(map[string]any)
	if !ok {
		t.Fatalf("SafeDescriptorMaps[0] is %T, not map[string]any", maps[0])
	}

	// Must NOT contain "env" key.
	if _, hasEnv := m["env"]; hasEnv {
		t.Errorf("SafeDescriptorMaps includes 'env' field (spec §1606 violation)")
	}

	// Must contain the four safe keys.
	for _, key := range []string{"alias", "kind", "display_name", "setup_hint"} {
		if _, ok := m[key]; !ok {
			t.Errorf("SafeDescriptorMaps missing key %q", key)
		}
	}

	// alias must not be the raw env var name.
	if alias, _ := m["alias"].(string); alias == "GUM_FLIGHTS_SESSION" {
		t.Errorf("alias is the raw env var name (spec §1606 violation): %q", alias)
	}
}
