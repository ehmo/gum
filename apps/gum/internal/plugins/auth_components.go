// Plugin auth prerequisites (docs/plugin-contract.md "Credential
// descriptors", spec.md §7).
//
// `needs_user_creds` plus `credential_descriptors` covers the secrets
// `gum plugin setup` can collect. A plugin whose product setup needs more
// than a secret value, such as an approved Google Ads developer token or a
// billing-enabled account, declares those steps as `auth_components[]`
// entries. Components marked `external` are steps gum can name but cannot
// perform, so setup prints them as a checklist instead of prompting.

package plugins

import (
	"fmt"
	"io"

	"github.com/ehmo/gum/internal/catalog"
)

// ValidateAuthComponents rejects a manifest whose declared prerequisite
// kinds are not on the §7 closed enum. `x-` prefixed kinds are accepted as
// informational per spec §1415. The returned error wraps
// catalog.ErrUnknownAuthComponent so the CLI renders AUTH_COMPONENT_UNKNOWN.
func ValidateAuthComponents(pluginID string, components []catalog.AuthComponent) error {
	for _, comp := range components {
		if comp.Kind == "" || !comp.Kind.Valid() {
			return fmt.Errorf("plugin %s: auth_components kind %q: %w", pluginID, comp.Kind, catalog.ErrUnknownAuthComponent)
		}
	}
	return nil
}

// ExternalAuthComponents returns the declared components gum cannot complete,
// in declaration order.
func ExternalAuthComponents(components []catalog.AuthComponent) []catalog.AuthComponent {
	var out []catalog.AuthComponent
	for _, comp := range components {
		if comp.External {
			out = append(out, comp)
		}
	}
	return out
}

// writeExternalChecklist prints the external prerequisites as unchecked
// checklist items. The output names the component kind and its setup hint;
// neither carries a secret value or a raw env var name, which is what keeps
// this safe to print under spec §1414.
//
// Nothing is written when the plugin declares no external component, so a
// plugin that needs only secrets keeps its current setup output.
func writeExternalChecklist(w io.Writer, pluginID string, components []catalog.AuthComponent) {
	external := ExternalAuthComponents(components)
	if w == nil || len(external) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "Prerequisites gum cannot complete for %q:\n", pluginID)
	for _, comp := range external {
		suffix := ""
		if comp.Optional {
			suffix = " (optional)"
		}
		line := fmt.Sprintf("  [ ] %s%s", comp.Kind, suffix)
		if comp.SetupHint != "" {
			line += ": " + comp.SetupHint
		}
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprintln(w)
}
