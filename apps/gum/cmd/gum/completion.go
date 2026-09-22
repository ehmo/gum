package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
	profilepkg "github.com/ehmo/gum/internal/profile"
	"github.com/spf13/cobra"
)

// completeOpIDByRisk returns a cobra completion function that proposes op_ids
// from the embedded catalog whose default variant matches the given risk class.
// Pass an empty string to allow any risk class (used for `gum describe`).
func completeOpIDByRisk(riskClass string) func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		snap := loadCatalog()
		if snap == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []string
		want := catalog.RiskClass(riskClass)
		for i := range snap.Ops {
			op := &snap.Ops[i]
			if riskClass != "" {
				v := defaultVariant(op)
				if v == nil || v.RiskClass != want {
					continue
				}
			}
			if toComplete == "" || strings.HasPrefix(op.OpID, toComplete) {
				out = append(out, op.OpID)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// defaultVariant returns the variant whose VariantID matches op.DefaultVariantID,
// or the first variant if no match is found.
func defaultVariant(op *catalog.Op) *catalog.Variant {
	for i := range op.Variants {
		if op.Variants[i].VariantID == op.DefaultVariantID {
			return &op.Variants[i]
		}
	}
	if len(op.Variants) > 0 {
		return &op.Variants[0]
	}
	return nil
}

// completionCap is the spec §13 ceiling on completion candidates. It applies
// to every completer here, not only the catalog-backed ones: a profile
// directory or a plugin registry can grow past what a shell menu can show.
const completionCap = 50

// completeProfileNames proposes the profiles that exist on disk, one per
// directory under <config home>/gum. It reads the filesystem only; spec §13
// forbids a completion handler from making an upstream call.
//
// "default" is always offered even with no directory for it, because the
// implicit profile needs no directory until something writes config.
func completeProfileNames(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	dir, err := profilepkg.DefaultName.ConfigPath()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	// ConfigPath is <base>/gum/<profile>/config.toml; step up twice.
	root := filepath.Dir(filepath.Dir(dir))

	seen := map[string]bool{}
	var out []string
	consider := func(name string) {
		if seen[name] || !strings.HasPrefix(name, toComplete) {
			return
		}
		if _, err := profilepkg.Parse(name); err != nil {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	consider(profilepkg.DefaultName.String())

	entries, err := os.ReadDir(root)
	if err != nil {
		return capCompletions(out), cobra.ShellCompDirectiveNoFileComp
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		consider(e.Name())
	}
	sort.Strings(out)
	return capCompletions(out), cobra.ShellCompDirectiveNoFileComp
}

// completePluginNames proposes the plugin IDs in the active profile's
// plugin-state.json. Inventory-only plugins are included on purpose: every
// subcommand that takes a plugin ID (remove, reload, unquarantine, setup, run)
// acts on inventory, so hiding a pending-restart or quarantined plugin would
// hide exactly the one the operator is trying to fix.
func completePluginNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Only the first positional argument is a plugin ID; `gum plugin run`
	// takes a tool name and an args JSON after it.
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	profileDir, err := resolveProfileDir(resolveProfileFlag(cmd))
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	rows, err := plugins.InventoryRows(registry.New(profileDir))
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, r := range rows {
		if strings.HasPrefix(r.Name, toComplete) {
			out = append(out, r.Name)
		}
	}
	return capCompletions(out), cobra.ShellCompDirectiveNoFileComp
}

func capCompletions(vals []string) []string {
	if len(vals) > completionCap {
		return vals[:completionCap]
	}
	return vals
}
