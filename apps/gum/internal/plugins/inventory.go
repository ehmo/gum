package plugins

import (
	"sort"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// InventoryRow is one plugin-state.json row projected for reporting: the
// lifecycle status plus the supervisor's crash-recovery fields.
//
// `gum plugin list` merges these rows with the installed manifests. Manifests
// alone hide the plugins an operator most needs to see: a plugin quarantined
// with PLUGIN_MANIFEST_NOT_FOUND has no loadable manifest by definition, and
// Host.List skips every directory whose manifest fails to load, so a
// manifest-only listing skips exactly the broken ones (gum-mmzr).
type InventoryRow struct {
	// Name is the plugin ID. It is the same key every other plugin
	// subcommand takes and the same key the supervisor writes crashes under.
	Name string
	// Status is the §8.7 lifecycle status: StatusActive,
	// StatusInstalledPendingRestart, or "" on rows written before the field
	// existed.
	Status string
	State  SupervisorState
}

// InventoryRows returns every row in plugin-state.json, sorted by name. Rows
// that are not JSON objects, or that carry no name, are skipped: a malformed
// row must not hide the rows around it.
func InventoryRows(reg *registry.Registry) ([]InventoryRow, error) {
	files, err := reg.Load()
	if err != nil {
		return nil, err
	}
	var out []InventoryRow
	for _, raw := range files.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := row["name"].(string)
		if name == "" {
			continue
		}
		status, _ := row["status"].(string)
		out = append(out, InventoryRow{
			Name:   name,
			Status: status,
			State:  decodeSupervisorState(row),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
