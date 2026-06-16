package plugins

import (
	"context"
	"time"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// StatusInstalledPendingRestart marks a plugin row that was added during a
// running MCP session: it MUST appear in the inventory (so operators can
// confirm the install succeeded) but it MUST be excluded from any surface
// that would invoke the plugin (gum.search_apis, gum.describe_op, MCP
// completions) until the next process boot promotes it to active (spec
// §8.7 + §13 line 3148).
const StatusInstalledPendingRestart = "installed_pending_restart"

// StatusActive is the steady-state row status for an installed plugin that
// the current process is permitted to dispatch. Promotion from
// StatusInstalledPendingRestart → StatusActive happens at boot via
// PromotePendingRestart.
const StatusActive = "active"

// MarkInstalledPendingRestart writes (or updates) the plugin-state.json row
// for pluginName with status=installed_pending_restart and installed_at=now.
// Existing supervisor fields (quarantined, retry_count, backoff_step) are
// left untouched so a re-install does not silently clear quarantine.
//
// Spec §8.7: a newly installed plugin is inventory-visible (a row exists)
// but not runtime-active (status pins it out of completions) until the next
// process boot.
func MarkInstalledPendingRestart(ctx context.Context, reg *registry.Registry, pluginName string, now time.Time) error {
	return reg.WriteTransaction(ctx, func(f *registry.Files) error {
		row, idx := findOrAppendRow(f, pluginName)
		row["status"] = StatusInstalledPendingRestart
		if _, ok := row["installed_at"]; !ok {
			row["installed_at"] = now.UTC().Format(time.RFC3339)
		}
		// re-installing a previously active plugin must demote it back to
		// pending so the running process keeps dispatching the old code path
		// (it does not pick up the new binary until the next boot).
		delete(row, "activated_at")
		f.State.Plugins[idx] = row
		return nil
	})
}

// PromotePendingRestart scans plugin-state.json and flips every row whose
// status is installed_pending_restart to status=active, stamping
// activated_at=now. Called at process boot so plugins installed during a
// prior session become runtime-active for this session.
//
// Returns the names of promoted plugins so the caller can log a structured
// "plugin promoted" event per row.
func PromotePendingRestart(ctx context.Context, reg *registry.Registry, now time.Time) ([]string, error) {
	var promoted []string
	err := reg.WriteTransaction(ctx, func(f *registry.Files) error {
		for i, raw := range f.State.Plugins {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			status, _ := row["status"].(string)
			if status != StatusInstalledPendingRestart {
				continue
			}
			name, _ := row["name"].(string)
			row["status"] = StatusActive
			row["activated_at"] = now.UTC().Format(time.RFC3339)
			f.State.Plugins[i] = row
			if name != "" {
				promoted = append(promoted, name)
			}
		}
		return nil
	})
	return promoted, err
}

// ActivePluginNames returns the plugin names currently eligible for runtime
// dispatch — every row whose status is anything other than
// installed_pending_restart. Quarantined plugins are still returned by this
// helper (the supervisor decides whether to spawn them); only the
// pending-restart filter is applied here.
//
// Used by the MCP roster filter (gum.search_apis, completion handlers) and
// by callers that need to honour spec §8.7's "not invokable until restart"
// rule without re-implementing the JSON scan.
func ActivePluginNames(reg *registry.Registry) ([]string, error) {
	files, err := reg.Load()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, raw := range files.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		status, _ := row["status"].(string)
		if status == StatusInstalledPendingRestart {
			continue
		}
		if name, _ := row["name"].(string); name != "" {
			out = append(out, name)
		}
	}
	return out, nil
}

// InventoryPluginNames returns every plugin name in plugin-state.json,
// including installed_pending_restart rows. Used by the CLI `gum plugin
// list` so operators can confirm a fresh install landed before they
// restart.
func InventoryPluginNames(reg *registry.Registry) ([]string, error) {
	files, err := reg.Load()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, raw := range files.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := row["name"].(string); name != "" {
			out = append(out, name)
		}
	}
	return out, nil
}
