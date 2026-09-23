package plugins

import (
	"errors"
	"fmt"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// ErrIncompleteGeneration reports that the profile's three registry files do
// not agree on one install_generation. Spec §8.7 step 5 forbids dispatching
// from an incomplete generation, so the session snapshot keeps the embedded
// catalog alone until a later transaction republishes all three files.
var ErrIncompleteGeneration = errors.New("plugins: incomplete install generation")

// ActivePluginNames returns the plugins this process may dispatch: the
// plugin-state.json rows whose status is "active" and that are not
// quarantined (spec §8.7 step 2).
//
// installed_pending_restart and needs_configuration stay out by construction.
// Both are visible in the inventory and both refuse to run, so a row in
// either state must not reach a snapshot the dispatcher resolves against.
func ActivePluginNames(files *registry.Files) map[string]bool {
	if files == nil || files.State == nil {
		return nil
	}
	out := make(map[string]bool, len(files.State.Plugins))
	for _, raw := range files.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := row["name"].(string)
		if name == "" {
			continue
		}
		if status, _ := row["status"].(string); status != StatusActive {
			continue
		}
		if quarantined, _ := row["quarantined"].(bool); quarantined {
			continue
		}
		out[name] = true
	}
	return out
}

// SessionCatalog returns base plus every active plugin variant recorded in
// the profile reg is bound to (spec §5).
//
// It is the one seam that builds a session snapshot: the CLI dispatcher and
// the MCP server both take their snapshot from a single call, so `gum call`
// and gum://op/{id} can never disagree about which plugin ops exist.
//
// Callers boot on the returned snapshot whatever the error says. A profile
// whose registry cannot be read still has to run the built-in catalog, so
// every failure path returns base unchanged rather than nil. refused carries
// one error per rejected row; err carries the failure that stopped the merge
// from running at all.
func SessionCatalog(base *catalog.Catalog, reg *registry.Registry) (snapshot *catalog.Catalog, refused []error, err error) {
	if base == nil || reg == nil {
		return base, nil, nil
	}

	gen, files, err := reg.SelectGenerationFiles()
	if err != nil {
		return base, nil, fmt.Errorf("plugins: read registry: %w", err)
	}
	if !gen.Ok {
		return base, nil, ErrIncompleteGeneration
	}

	merged, refused := catalog.MergePluginVariants(base, files.Catalog, ActivePluginNames(files))
	return merged, refused, nil
}
