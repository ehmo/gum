package plugins

import (
	"errors"
	"fmt"

	"github.com/ehmo/gum/internal/catalog"
)

// ErrPluginNamespaceConflict is the host-side rendering of spec §11 sentinel
// PLUGIN_NAMESPACE_CONFLICT. Spec §5.1 ("Third-party namespace ownership")
// requires every third-party plugin to declare `namespace_owner` and forbids
// reusing a prefix already locked to a different owner unless the user opts
// in via --dev-allow-namespace-conflict on a dev profile.
var ErrPluginNamespaceConflict = errors.New("PLUGIN_NAMESPACE_CONFLICT")

// LookupNamespaceOwner returns the owner string previously locked to prefix
// in the given PluginsLock, plus a found flag. The lock is profile-scoped
// per spec §5.1; callers MUST pass the active profile's lock — there is no
// cross-profile merge.
func LookupNamespaceOwner(lock *catalog.PluginsLock, prefix string) (string, bool) {
	if lock == nil {
		return "", false
	}
	for _, raw := range lock.Plugins {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		gotPrefix, _ := m["prefix"].(string)
		if gotPrefix != prefix {
			continue
		}
		owner, _ := m["namespace_owner"].(string)
		return owner, owner != ""
	}
	return "", false
}

// RecordNamespaceOwner inserts or updates the prefix→owner row in lock. It
// is the post-validation companion to ValidateNamespaceOwnership; callers
// run it inside the same registry write transaction that stamps the new
// (install_generation, install_txid) pair.
func RecordNamespaceOwner(lock *catalog.PluginsLock, prefix, owner string) {
	if lock == nil {
		return
	}
	for i, raw := range lock.Plugins {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := m["prefix"].(string); got == prefix {
			m["namespace_owner"] = owner
			lock.Plugins[i] = m
			return
		}
	}
	lock.Plugins = append(lock.Plugins, map[string]any{
		"prefix":          prefix,
		"namespace_owner": owner,
	})
}

// NamespaceOptions carries the install-time flags that govern the dev-profile
// override of PLUGIN_NAMESPACE_CONFLICT. Spec §5.1: the override is honored
// only when both flags are true; either alone keeps the conflict fatal.
type NamespaceOptions struct {
	ProfileIsDev          bool // true when the active profile is marked dev
	AllowConflictOverride bool // true when --dev-allow-namespace-conflict was passed
}

// ValidateNamespaceOwnership enforces the spec §5.1 install-time rules. It
// fires PLUGIN_NAMESPACE_CONFLICT when:
//
//  1. The manifest's namespace_owner is empty (third-party manifests MUST
//     declare it).
//  2. The prefix is already locked to a different owner AND the dev override
//     is not active (both ProfileIsDev and AllowConflictOverride must be set
//     to allow the override).
//
// Matching owners are always accepted as upgrade/reinstall (spec §5.1: "A
// later plugin may reuse that prefix only when the owner string matches
// exactly"), without consulting the override flags.
//
// Callers that want to record the new owner on success should follow this
// with RecordNamespaceOwner inside the same registry write transaction.
func ValidateNamespaceOwnership(prefix, declaredOwner string, lock *catalog.PluginsLock, opts NamespaceOptions) error {
	if declaredOwner == "" {
		return fmt.Errorf("%w: manifest is missing namespace_owner for prefix %q", ErrPluginNamespaceConflict, prefix)
	}
	existing, found := LookupNamespaceOwner(lock, prefix)
	if !found {
		return nil
	}
	if existing == declaredOwner {
		return nil
	}
	if opts.ProfileIsDev && opts.AllowConflictOverride {
		return nil
	}
	return fmt.Errorf("%w: prefix %q is locked to %q; manifest declares %q",
		ErrPluginNamespaceConflict, prefix, existing, declaredOwner)
}
