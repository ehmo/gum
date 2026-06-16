package auth

import (
	"errors"

	keyring "github.com/zalando/go-keyring"
)

// keyringService is the constant service name used for all gum secrets in the
// OS keychain. Per spec §7, the service name is `gum`; per-credential
// uniqueness is encoded in the user key (e.g. `gum.byo_oauth.<scope-hash>`).
const keyringService = "gum"

// OSKeyring is the production KeyringBackend backed by github.com/zalando/go-keyring.
// On macOS this writes to Keychain Services; on Linux, libsecret
// (gnome-keyring / kwallet via D-Bus). Platforms without a
// supported backend surface AUTH_KEYCHAIN_UNAVAILABLE (spec §7) so callers can
// guide the user to install libsecret or pick a different machine instead of
// silently falling back to plaintext storage.
type OSKeyring struct{}

// NewOSKeyring returns the platform-native keychain backend.
func NewOSKeyring() *OSKeyring { return &OSKeyring{} }

// Get retrieves a secret stored under key. Returns ("", nil) when the key is
// absent (matches the in-memory test backend behavior); returns
// AUTH_KEYCHAIN_UNAVAILABLE when the OS backend is missing or returns an
// error other than "not found".
func (OSKeyring) Get(key string) (string, error) {
	v, err := keyring.Get(keyringService, key)
	if err == nil {
		return v, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return "", &AuthError{
		Code:             "AUTH_KEYCHAIN_UNAVAILABLE",
		Strategy:         "", // generic keychain backend: strategy-agnostic storage error
		HumanRemediation: keychainHumanHint(err),
	}
}

// Set stores value under key in the OS keychain. Returns
// AUTH_KEYCHAIN_UNAVAILABLE if the backend is missing.
func (OSKeyring) Set(key, value string) error {
	if err := keyring.Set(keyringService, key, value); err != nil {
		return &AuthError{
			Code:             "AUTH_KEYCHAIN_UNAVAILABLE",
			Strategy:         "", // generic keychain backend: strategy-agnostic storage error
			HumanRemediation: keychainHumanHint(err),
		}
	}
	return nil
}

// Delete removes the secret stored under key. Absent keys are not an error.
func (OSKeyring) Delete(key string) error {
	err := keyring.Delete(keyringService, key)
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return &AuthError{
		Code:             "AUTH_KEYCHAIN_UNAVAILABLE",
		Strategy:         "", // generic keychain backend: strategy-agnostic storage error
		HumanRemediation: keychainHumanHint(err),
	}
}

// keychainHumanHint surfaces the OS-specific install advice when the keychain
// backend is missing, falling back to the underlying error message otherwise.
func keychainHumanHint(err error) string {
	if errors.Is(err, keyring.ErrUnsupportedPlatform) {
		return "OS keychain backend not available; on Linux install libsecret (`apt install libsecret-1-0` / `dnf install libsecret`) or use macOS"
	}
	return "keychain operation failed: " + err.Error()
}

// Compile-time check that OSKeyring satisfies the KeyringBackend interface
// consumed by ByoOAuth and the (future) gum_oauth flow.
var _ KeyringBackend = (*OSKeyring)(nil)
