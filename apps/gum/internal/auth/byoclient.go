package auth

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ByoClient is a user-supplied ("bring your own") OAuth 2.0 client — the
// client_id (and optional client_secret) of a Desktop-app credential the
// operator created once in the Google Cloud console. gum runs the loopback
// PKCE flow against it, so no gcloud and no managed-client verification is
// needed. ClientSecret may be empty for a public PKCE client.
type ByoClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// byoClientKeyringKey returns the per-profile OS-keychain key for the stored
// BYO client. Per-profile so dev/prod credentials don't collide, matching the
// api_key storage convention.
func byoClientKeyringKey(profile string) string {
	p := strings.TrimSpace(profile)
	if p == "" {
		p = DefaultAPIKeyProfile
	}
	return fmt.Sprintf("gum.byo_oauth_client.%s", p)
}

// StoreByoClient persists client under the per-profile keyring entry. The
// client_id is mandatory; the secret is optional (public PKCE clients omit
// it). Returns AUTH_KEYCHAIN_UNAVAILABLE when no backend is configured and a
// plain error when the client_id is empty.
func StoreByoClient(kb KeyringBackend, profile string, client ByoClient) error {
	if kb == nil {
		return &AuthError{
			Code:             "AUTH_KEYCHAIN_UNAVAILABLE",
			Strategy:         "byo_oauth",
			HumanRemediation: "no keyring backend configured; cannot store the OAuth client",
		}
	}
	if strings.TrimSpace(client.ClientID) == "" {
		return fmt.Errorf("byo_oauth: client_id is required")
	}
	blob, err := json.Marshal(client)
	if err != nil {
		return fmt.Errorf("byo_oauth: marshal client: %w", err)
	}
	return kb.Set(byoClientKeyringKey(profile), string(blob))
}

// LoadByoClient returns the persisted BYO client for profile. ok is false when
// no client is configured (the caller should route to
// `gum auth use-oauth-client`); err is reserved for keyring/decode failures.
func LoadByoClient(kb KeyringBackend, profile string) (client ByoClient, ok bool, err error) {
	if kb == nil {
		return ByoClient{}, false, nil
	}
	blob, err := kb.Get(byoClientKeyringKey(profile))
	if err != nil {
		return ByoClient{}, false, err
	}
	if strings.TrimSpace(blob) == "" {
		return ByoClient{}, false, nil
	}
	if err := json.Unmarshal([]byte(blob), &client); err != nil {
		return ByoClient{}, false, fmt.Errorf("byo_oauth: decode stored client: %w", err)
	}
	if client.ClientID == "" {
		return ByoClient{}, false, nil
	}
	return client, true, nil
}

// DeleteByoClient removes the persisted BYO client for profile. Absent entries
// are not an error.
func DeleteByoClient(kb KeyringBackend, profile string) error {
	if kb == nil {
		return nil
	}
	return kb.Delete(byoClientKeyringKey(profile))
}
