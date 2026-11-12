package auth

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/embedded"
)

// A storage timeout must not ask the caller to authorize scopes already granted.
func TestGrantedScopesTimeoutDispatchEnvelope(t *testing.T) {
	for _, stage := range []string{"client", "grant"} {
		t.Run(stage, func(t *testing.T) {
			blockKeyring(t)
			t.Setenv("GUM_KEYRING_TIMEOUT", "")
			if stage == "grant" {
				blockedGet := keyringGet
				keyringGet = func(service, key string) (string, error) {
					if key == byoClientKeyringKey("default") {
						return `{"client_id":"client-xyz"}`, nil
					}
					return blockedGet(service, key)
				}
			}
			var cat catalog.Catalog
			if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
				t.Fatal(err)
			}
			d := dispatch.NewDispatcherWithConfig(&cat, nil, dispatch.DispatcherConfig{
				Policy: dispatch.ProfilePolicy{LoadScopes: func() ([]string, error) {
					return GrantedScopes(NewBestEffortOSKeyring(), "default")
				}},
			})
			_, err := d.Dispatch(t.Context(), &dispatch.Invocation{
				OpID: "searchconsole.sites.list", Args: map[string]any{},
			})
			var se *dispatch.StructuredError
			if !errors.As(err, &se) || se.ErrCode != "AUTH_KEYCHAIN_UNAVAILABLE" {
				t.Fatalf("dispatch error = %v; want AUTH_KEYCHAIN_UNAVAILABLE", err)
			}
			if !se.Retryable {
				t.Fatal("Keychain timeout must be retryable")
			}
			// The same dispatcher must reread on retry, rather than retain the
			// startup timeout. A readable grant lacking this scope is SCOPE_MISSING.
			keyringGet = func(_ string, key string) (string, error) {
				if key == byoClientKeyringKey("default") {
					return `{"client_id":"client-xyz"}`, nil
				}
				return `{"refresh_token":"test","scopes":["https://www.googleapis.com/auth/gmail.readonly"]}`, nil
			}
			_, err = d.Dispatch(t.Context(), &dispatch.Invocation{
				OpID: "searchconsole.sites.list", Args: map[string]any{},
			})
			if !errors.As(err, &se) || se.ErrCode != dispatch.ErrCodeScopeMissing || se.Retryable {
				t.Fatalf("retry with a readable grant lacking the required scope = %v; want non-retryable SCOPE_MISSING", err)
			}
		})
	}
}
