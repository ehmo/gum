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
// §8.7 + §13).
const StatusInstalledPendingRestart = "installed_pending_restart"

// StatusActive is the steady-state row status for an installed plugin that
// the current process is permitted to dispatch. Promotion from
// StatusInstalledPendingRestart → StatusActive happens at boot via
// PromotePendingRestart.
const StatusActive = "active"

// StatusNeedsConfiguration marks a plugin whose manifest declares
// needs_user_creds that no `gum plugin setup` run has stored yet. Boot
// promotion skips these rows: the credential prompt and its live canary are
// what clear the status (spec §8.7 step 2 + §7).
const StatusNeedsConfiguration = "needs_configuration"

// PromotePendingRestart scans plugin-state.json and flips every row whose
// status is installed_pending_restart to status=active, stamping
// activated_at=now. Called at process boot so plugins installed during a
// prior session become runtime-active for this session.
//
// Returns the names of promoted plugins so the caller can log a structured
// "plugin promoted" event per row.
func PromotePendingRestart(ctx context.Context, reg *registry.Registry, now time.Time) ([]string, error) {
	// Every startup calls this, so a registry with nothing to promote must not
	// take the install lock or burn an install_generation.
	pending, err := hasPendingRestart(reg)
	if err != nil || !pending {
		return nil, err
	}

	var promoted []string
	err = reg.WriteTransaction(ctx, func(f *registry.Files) error {
		for i, raw := range f.State.Plugins {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if !promotableAtStartup(row) {
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

// hasPendingRestart reports whether any row is promotable, reading the files
// directly so the caller can skip the write transaction entirely.
func hasPendingRestart(reg *registry.Registry) (bool, error) {
	files, err := reg.Load()
	if err != nil {
		return false, err
	}
	for _, raw := range files.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if promotableAtStartup(row) {
			return true, nil
		}
	}
	return false, nil
}

// promotableAtStartup is the spec §8.7 startup-activation filter: a
// pending-restart row that is not quarantined. A quarantined plugin keeps its
// pending status so the operator clears the quarantine first; promoting it
// would hand the supervisor a row that claims to be active and still refuses
// to spawn. needs_configuration rows carry a different status and never match.
func promotableAtStartup(row map[string]any) bool {
	if status, _ := row["status"].(string); status != StatusInstalledPendingRestart {
		return false
	}
	quarantined, _ := row["quarantined"].(bool)
	return !quarantined
}
