// Spec §12 line 2537: `gum plugin list --format=json` emits
// `{"plugins": PluginInventoryRow[]}`, whose "rows mirror gum://plugins
// columns with named JSON fields". Spec §13 line 3234 fixes those columns at
// [name, version, shape, status, tos, risk, variant_count].
//
// Both surfaces read one loader here, so the CLI and the MCP resource cannot
// drift in what they report or in where each field comes from. They differ in
// exactly one place, and it is a §13 requirement: gum://plugins drops
// installed_pending_restart rows, because those plugins cannot dispatch in the
// active session. The CLI keeps them, because an operator has to see the
// plugin that is waiting on a restart.
//
// variant_count on an inventory row is the lockfile's assertion, not a count
// of plugin-catalog.json rows. The two can disagree, and the per-plugin record
// LoadPluginInfo assembles is where that shows up: it counts the catalog and
// sets metadata_warning="lock_catalog_mismatch" when the lockfile claims a
// different number. An inventory row has no field to carry that warning, so it
// reports the lockfile figure and leaves the reconciliation to
// `gum plugin info` / `gum://plugin/{name}`.

package mcp

import (
	"path/filepath"
	"sort"
)

// PluginInventoryRow is one row of the plugin inventory. Field order matches
// the §13 line 3234 column order; JSON names are the spec's column names.
type PluginInventoryRow struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Shape        string `json:"shape"`
	Status       string `json:"status"`
	ToS          string `json:"tos"`
	Risk         string `json:"risk"`
	VariantCount int    `json:"variant_count"`
}

// LoadPluginInventory returns every installed plugin in profileDir, sorted by
// name, with no status filtering. A plugin needs a plugin-state.json row to
// appear: that file carries the status column, and a lock row without one is a
// half-written install rather than an installed plugin. A missing or malformed
// registry file yields no rows rather than an error, which is the
// fresh-install case neither caller has anything useful to say about.
func LoadPluginInventory(profileDir string) []PluginInventoryRow {
	if profileDir == "" {
		return nil
	}

	stateRows := loadPluginRowsFromFile(filepath.Join(profileDir, "plugin-state.json"))
	lockRows := loadPluginRowsFromFile(filepath.Join(profileDir, "plugins.lock"))

	lockByName := make(map[string]map[string]any, len(lockRows))
	for _, row := range lockRows {
		if name, _ := row["name"].(string); name != "" {
			lockByName[name] = row
		}
	}

	out := make([]PluginInventoryRow, 0, len(stateRows))
	for _, row := range stateRows {
		name, _ := row["name"].(string)
		if name == "" {
			continue
		}
		lock := lockByName[name]
		out = append(out, PluginInventoryRow{
			Name:         name,
			Version:      stringFromRow(lock, "version"),
			Shape:        stringFromRow(lock, "shape"),
			Status:       resolvePluginStatus(row),
			ToS:          stringFromRow(lock, "tos"),
			Risk:         stringFromRow(lock, "risk"),
			VariantCount: intFromRow(lock, "variant_count"),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
