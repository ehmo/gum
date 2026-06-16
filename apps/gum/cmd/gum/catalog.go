package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/spf13/cobra"
)

func newCatalogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Inspect the resolved catalog",
	}
	parentHelpOnly(cmd)
	cmd.AddCommand(newCatalogListOverridesCmd())
	return cmd
}

func newCatalogListOverridesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-overrides",
		Short: "List all variants with risk_override=true from the resolved catalog",
		RunE:  runCatalogListOverrides,
	}
}

// overrideEntry holds the three fields emitted per NDJSON line.
type overrideEntry struct {
	VariantID          string `json:"variant_id"`
	RiskClass          string `json:"risk_class"`
	RiskOverrideReason string `json:"risk_override_reason"`
}

func runCatalogListOverrides(cmd *cobra.Command, _ []string) error {
	name, err := resolveProfileName(cmd)
	if err != nil {
		return err
	}
	profile := name.String()

	// Build merged map: variant_id → overrideEntry.
	// Embedded catalog is seeded first; plugin-catalog overrides on collision.
	merged := map[string]overrideEntry{}

	// Seed from embedded catalog.
	if c := loadCatalog(); c != nil {
		for _, op := range c.Ops {
			for _, v := range op.Variants {
				if v.RiskOverride {
					merged[v.VariantID] = overrideEntry{
						VariantID:          v.VariantID,
						RiskClass:          string(v.RiskClass),
						RiskOverrideReason: v.RiskOverrideReason,
					}
				}
			}
		}
	}

	dataDir, err := name.DataDir()
	if err != nil {
		return fmt.Errorf("catalog list-overrides: could not determine data dir: %w", err)
	}
	pluginCatalogPath := filepath.Join(dataDir, "plugin-catalog.json")

	// Load plugin-catalog.json — missing file is treated as empty v1 catalog.
	data, err := os.ReadFile(pluginCatalogPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("catalog list-overrides: reading plugin-catalog.json: %w", err)
	}

	if len(data) > 0 {
		pc, err := catalog.LoadPluginCatalog(data)
		if err != nil {
			// Probe for the rejected version number to include in the error message.
			var probe struct {
				PluginCatalogSchemaVersion int `json:"plugin_catalog_schema_version"`
			}
			_ = json.Unmarshal(data, &probe)
			if errors.Is(err, catalog.ErrUnsupportedPluginCatalogSchemaVersion) {
				return fmt.Errorf("PLUGIN_CATALOG_SCHEMA_UNSUPPORTED: profile %q plugin_catalog_schema_version=%d not supported",
					profile, probe.PluginCatalogSchemaVersion)
			}
			return fmt.Errorf("catalog list-overrides: %w", err)
		}

		// Merge plugin variants — plugin takes precedence on collision.
		for _, raw := range pc.Variants {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			vid, _ := m["variant_id"].(string)
			if vid == "" {
				continue
			}
			riskOverride, _ := m["risk_override"].(bool)
			if !riskOverride {
				// Plugin entry with risk_override=false: remove from merged if present.
				// (The plugin is explicitly marking it as not an override; but spec says
				// plugin takes precedence, so if they set false, we remove any embedded entry.)
				delete(merged, vid)
				continue
			}
			rc, _ := m["risk_class"].(string)
			reason, _ := m["risk_override_reason"].(string)
			merged[vid] = overrideEntry{
				VariantID:          vid,
				RiskClass:          rc,
				RiskOverrideReason: reason,
			}
		}
	}

	if len(merged) == 0 {
		// Keep stdout empty for pipes; note on stderr only for an interactive
		// human so an empty result isn't mistaken for a silent failure, while
		// piped/scripted output stays clean (gum-s985).
		if isTerminal(cmd.ErrOrStderr()) {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No risk-class overrides in the active catalog.")
		}
		return nil
	}

	// Collect and sort by variant_id ascending.
	entries := make([]overrideEntry, 0, len(merged))
	for _, e := range merged {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].VariantID < entries[j].VariantID
	})

	// Emit one JSON line per entry with exactly three keys.
	out := cmd.OutOrStdout()
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("catalog list-overrides: encoding output: %w", err)
		}
	}
	return nil
}
