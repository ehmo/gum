package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/ehmo/gum/internal/mcp"
	"github.com/ehmo/gum/internal/output/jcs"
	"github.com/spf13/cobra"
)

// newPluginInfoCmd implements `gum plugin info <name>` (spec §12 for
// the roster, line 2520 for the JSON root). It reads only the profile's §8.7
// registry files, so it needs no plugin host and answers for a plugin that
// cannot spawn — which is the state an operator most needs to inspect.
func newPluginInfoCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "info <name>",
		Short: "Show the full record for one installed plugin",
		Long: `Prints the plugin record assembled from plugins.lock, plugin-state.json and
plugin-catalog.json for the active profile.

--format=json emits the same object the gum://plugin/<name> MCP resource
serves, byte for byte, so scripts can diff the two.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileDir, err := resolveProfileDir(resolveProfileFlag(cmd))
			if err != nil {
				return err
			}
			out, err := formatPluginInfo(profileDir, args[0], format)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text|json")
	return cmd
}

// formatPluginInfo renders one plugin record in the requested format. json is
// the JCS-canonical gum://plugin/{name} payload (spec §12: "Same object
// carried inside the gum://plugin/{name} JSON resource payload"), produced by
// the same loader and canonicaliser the resource handler uses. text is the
// human form and may change within a minor release, per the §12 contract.
func formatPluginInfo(profileDir, name, format string) (string, error) {
	if format != "text" && format != "json" {
		return "", fmt.Errorf("gum plugin info: unsupported --format %q; expected text or json", format)
	}

	rec, ok := mcp.LoadPluginInfo(profileDir, name)
	if !ok {
		// Same condition and same code the MCP resource answers with, so an
		// operator reading either surface sees one vocabulary.
		return "", fmt.Errorf("RESOURCE_NOT_FOUND: plugin %q is not installed in this profile", name)
	}

	if format == "json" {
		body, err := jcs.Marshal(rec)
		if err != nil {
			return "", fmt.Errorf("gum plugin info: canonical JSON encoding failed: %w", err)
		}
		return string(body) + "\n", nil
	}
	return renderPluginInfoText(rec), nil
}

// renderPluginInfoText lays the record out as aligned key/value rows. Optional
// fields are omitted when empty rather than shown as dashes, so the block
// length tracks what the profile actually knows; the nine fields required for
// every status always print.
func renderPluginInfoText(rec *mcp.PluginInfo) string {
	rows := [][2]string{
		{"name", rec.Name},
		{"version", dashIfEmpty(rec.Version)},
		{"status", dashIfEmpty(rec.Status)},
		{"shape", dashIfEmpty(rec.Shape)},
		{"tos", dashIfEmpty(rec.ToS)},
		{"risk", dashIfEmpty(rec.Risk)},
		{"namespace-owner", dashIfEmpty(rec.NamespaceOwner)},
		{"variants", fmt.Sprint(rec.VariantCount)},
		{"install-generation", fmt.Sprint(rec.InstallGeneration)},
	}
	rows = appendIfSet(rows, "installed-at", rec.InstalledAt)
	rows = appendIfSet(rows, "activated-at", rec.ActivatedAt)
	rows = appendIfSet(rows, "reason", rec.Reason)
	rows = appendIfSet(rows, "quarantined-at", rec.QuarantinedAt)
	rows = appendIfSet(rows, "last-error", rec.LastErrorCode)
	rows = appendIfSet(rows, "metadata-warning", rec.MetadataWarning)
	rows = appendIfSet(rows, "description", rec.Description)
	rows = appendIfSet(rows, "variant-ids", strings.Join(rec.VariantIDs, " "))
	for _, key := range []string{"source", "ref", "checksum"} {
		rows = appendIfSet(rows, "package."+key, stringField(rec.Package, key))
	}
	for _, key := range []string{"install_root", "executable_sha256"} {
		rows = appendIfSet(rows, "executable."+strings.ReplaceAll(key, "_", "-"), stringField(rec.Executable, key))
	}
	rows = appendIfSet(rows, "credentials-needed", credentialAliases(rec.CredentialDescriptors))

	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		_, _ = fmt.Fprintf(w, "%s\t%s\n", row[0], row[1])
	}
	_ = w.Flush()

	// A record is worth reading because the operator can act on it. Both
	// blocked states are cleared by a named command; print it.
	switch rec.Status {
	case "quarantined":
		fmt.Fprintf(&sb, "\nThis plugin refuses every call. Run `gum plugin reload %s` to retry the spawn, "+
			"or `gum plugin unquarantine %s` to clear the backoff without restarting.\n", rec.Name, rec.Name)
	case "needs_configuration":
		fmt.Fprintf(&sb, "\nThis plugin is missing credentials. Run `gum plugin setup %s`.\n", rec.Name)
	case "installed_pending_restart":
		fmt.Fprint(&sb, "\nThis plugin is installed but not active in running gum processes. Restart the MCP server to load it.\n")
	}
	return sb.String()
}

// appendIfSet adds a row only when the value is non-empty, keeping optional
// §13 fields out of the block instead of padding it with dashes.
func appendIfSet(rows [][2]string, key, value string) [][2]string {
	if value == "" {
		return rows
	}
	return append(rows, [2]string{key, value})
}

// stringField reads one string field out of a decoded JSON object, returning
// "" for a missing key or a non-string value.
func stringField(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	s, _ := obj[key].(string)
	return s
}

// credentialAliases lists the aliases a needs_configuration plugin is waiting
// on. Only the alias is printed: spec §7 forbids showing raw env names.
func credentialAliases(descs []any) string {
	out := make([]string, 0, len(descs))
	for _, raw := range descs {
		desc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if alias, _ := desc["alias"].(string); alias != "" {
			out = append(out, alias)
		}
	}
	return strings.Join(out, " ")
}
