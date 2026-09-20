// Spec §8.7 line 1893: "`gum plugin remove` MUST remove the corresponding
// entries under the same full-state transaction protocol." The legacy
// Host.Remove (host.go) is os.RemoveAll of the install directory and nothing
// else, so it leaves the plugin's catalog variants, its lock row and namespace
// lease, and its quarantine state behind. This file ships the registry-aware
// remove path that InstallWithRegistry is the other half of.

package plugins

import (
	"context"
	"fmt"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// RemoveOptions carries the registry binding used by RemoveWithRegistry. The
// Registry field is required; without it the caller wants Host.Remove.
type RemoveOptions struct {
	Registry *registry.Registry
}

// RemoveWithRegistry is the §8.7 remove protocol entry point: drop the
// plugin's rows from all three registry files in one transaction, then delete
// the install directory.
//
// Registry write precedes the file delete on purpose. A failure after the
// transaction leaves an install directory with no registry rows, which the
// next `gum plugin remove` or `gum plugin install` cleans up — the same
// recoverable state InstallWithRegistry documents for a crash between its file
// copy and its transaction. The reverse order would leave rows pointing at a
// missing executable, which is the state this function exists to prevent.
func (h *Host) RemoveWithRegistry(ctx context.Context, pluginID string, opts RemoveOptions) error {
	if opts.Registry == nil {
		return fmt.Errorf("plugin remove: registry is required for RemoveWithRegistry")
	}
	if !pluginIDRe.MatchString(pluginID) {
		return ErrManifestInvalid
	}

	err := opts.Registry.WriteTransaction(ctx, func(f *registry.Files) error {
		f.Catalog.Variants = dropRows(f.Catalog.Variants, func(row map[string]any) bool {
			owner, _ := row["owner_plugin"].(string)
			return owner == pluginID
		})
		// The lock row carries both `name` and `prefix`; the namespace lease
		// lives on the same row (RecordNamespaceOwner updates it in place), so
		// dropping the row releases the lease. A prefix-only row exists when
		// RecordNamespaceOwner ran without an install, so match either key.
		f.Lock.Plugins = dropRows(f.Lock.Plugins, func(row map[string]any) bool {
			name, _ := row["name"].(string)
			prefix, _ := row["prefix"].(string)
			return name == pluginID || prefix == pluginID
		})
		f.State.Plugins = dropRows(f.State.Plugins, func(row map[string]any) bool {
			name, _ := row["name"].(string)
			return name == pluginID
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("plugin remove: update registry: %w", err)
	}

	return h.Remove(ctx, pluginID)
}

// dropRows returns rows for which match reports false. A row that is not a
// JSON object is kept: it belongs to a schema this build does not model, and
// silently discarding it would lose registry state the next writer needs.
func dropRows(rows []any, match func(map[string]any) bool) []any {
	out := make([]any, 0, len(rows))
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if ok && match(row) {
			continue
		}
		out = append(out, raw)
	}
	return out
}
