// Spec §8.1 / docs/plugin-contract.md §needs_user_creds: a plugin manifest MUST
// NOT claim host-owned credential env var names. Without this gate a manifest
// could list GUM_OAUTH_CLIENT_SECRET under needs_user_creds and `gum plugin
// setup` would prompt the operator for it by display_name alone, never showing
// which variable was being harvested.

package plugins

import (
	"errors"
	"fmt"

	"github.com/ehmo/gum/internal/pluginenv"
)

// ErrPluginEnvProhibited is the spec §7 sentinel for a manifest that declares a
// denylisted env var name in needs_user_creds or declared_capabilities.env_allow.
var ErrPluginEnvProhibited = errors.New("PLUGIN_ENV_PROHIBITED")

// reservedCompoundEnvName is the one whitelisted needs_user_creds entry
// (spec §7 "Compound auth token forwarding"). The host substitutes the active
// access token at spawn; the plugin never sees a refresh token. It is spelled
// lowercase in the manifest and is not an env var name, so the denylist would
// not match it anyway; naming it here documents why.
const reservedCompoundEnvName = "google_access_token"

// ValidatePluginEnvNames rejects denylisted env var names in a manifest. Both
// declaration sites are covered: needs_user_creds, which drives the operator
// credential prompt, and declared_capabilities.env_allow, which drives spawn
// passthrough. Runtime already filters env_allow through pluginenv.IsDeniedEnv,
// so this gate turns a silently-dropped declaration into an install failure.
//
// The message form is fixed by docs/plugin-contract.md line 68 and spec §8.1
// line 1633 and is shared by the prefix rule and the exact-name denylist.
func ValidatePluginEnvNames(pluginID string, needs, envAllow []string) error {
	for _, name := range needs {
		if name == reservedCompoundEnvName {
			continue
		}
		if pluginenv.IsDeniedEnv(name) {
			return fmt.Errorf("%w: needs_user_creds entry '%s' on plugin '%s' is a prohibited env var name",
				ErrPluginEnvProhibited, name, pluginID)
		}
	}
	for _, name := range envAllow {
		if pluginenv.IsDeniedEnv(name) {
			return fmt.Errorf("%w: env_allow entry '%s' on plugin '%s' is a prohibited env var name",
				ErrPluginEnvProhibited, name, pluginID)
		}
	}
	return nil
}
