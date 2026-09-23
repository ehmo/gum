package plugins

import (
	"fmt"

	"github.com/ehmo/gum/internal/sanitize"
)

// pluginToolKind selects the convenience token budget (spec §5.4 rule 4) plus
// the rule 13 codepoint cap. A plugin tool is never a meta-tool: meta-tools are
// the nine Tier A surfaces the host owns, and a plugin reaches the model
// through gum.search_apis and gum.describe_op exactly like a first-party
// convenience op. It is not curator-authored either, which is what rule 13
// bounds.
const pluginToolKind = sanitize.ToolKindPlugin

// sanitizeToolDescription runs the §5.4 description sanitizer over one advertised
// tool. It is the plugin-side twin of cmd/gen-catalog's validateOpDescriptions:
// the first-party catalog is checked when it is generated, and a plugin
// manifest is checked when it is loaded, so every description the model sees
// has passed the same rule set.
//
// The whole description is one field here, so rule 6 and the rule 9-10
// cross-field re-scan run over it directly rather than over a joined title and
// summary.
func sanitizeToolDescription(tool ToolDecl) error {
	_, violations, err := sanitize.Sanitize(tool.Description, pluginToolKind, tool.RiskClass)
	if err != nil {
		return fmt.Errorf("%w: advertised_tools[%s].description: %v", ErrManifestInvalid, tool.Name, err)
	}
	if len(violations) == 0 {
		return nil
	}

	return fmt.Errorf("%w: advertised_tools[%s].description: rule %d: %s",
		ErrManifestInvalid, tool.Name, violations[0].Rule, violations[0].Reason)
}
