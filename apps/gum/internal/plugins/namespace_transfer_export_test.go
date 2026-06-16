// Test-only inspectors for namespace_transfer_test.go. These live in the
// production `plugins` package (not _test) so they can dip into the raw row
// shape that namespace_transfer.go writes; the `ForTest` suffix and the
// `_export_test.go` file pattern keep them out of the production API surface.

package plugins

import "github.com/ehmo/gum/internal/catalog"

// NamespaceOwnerForTest returns the raw namespace_owner string in lock for
// prefix without the not-found bool that LookupNamespaceOwner pins. Used by
// tests that need to assert the post-release empty state.
func NamespaceOwnerForTest(lock *catalog.PluginsLock, prefix string) string {
	if lock == nil {
		return ""
	}
	for _, raw := range lock.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := row["prefix"].(string); got == prefix {
			owner, _ := row["namespace_owner"].(string)
			return owner
		}
	}
	return ""
}

// TransferHistoryForTest exposes the transfer_history sub-array as a typed
// slice of maps. Returns nil when the prefix is unknown or has no history.
func TransferHistoryForTest(lock *catalog.PluginsLock, prefix string) []map[string]any {
	if lock == nil {
		return nil
	}
	for _, raw := range lock.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := row["prefix"].(string); got != prefix {
			continue
		}
		hist, ok := row["transfer_history"].([]any)
		if !ok {
			return nil
		}
		out := make([]map[string]any, 0, len(hist))
		for _, h := range hist {
			if m, ok := h.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}
