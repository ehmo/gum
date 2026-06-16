package plugins_test

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/plugins"
)

// TestPluginNamespaceOwnership covers docs/test-matrix.md line 91 and spec
// §5.1 third-party namespace ownership: matching owners may upgrade, mismatches
// fail with PLUGIN_NAMESPACE_CONFLICT, --dev-allow-namespace-conflict is
// honored only in dev profiles, and locks are profile-scoped.
func TestPluginNamespaceOwnership(t *testing.T) {
	t.Parallel()

	t.Run("new prefix records owner without conflict", func(t *testing.T) {
		t.Parallel()
		lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		err := plugins.ValidateNamespaceOwnership("fli", "io.example.flights", lock, plugins.NamespaceOptions{})
		if err != nil {
			t.Fatalf("first install on empty lock: %v", err)
		}
		plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")
		owner, found := plugins.LookupNamespaceOwner(lock, "fli")
		if !found || owner != "io.example.flights" {
			t.Fatalf("RecordNamespaceOwner did not persist: owner=%q found=%v", owner, found)
		}
	})

	t.Run("matching owner upgrade succeeds", func(t *testing.T) {
		t.Parallel()
		lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")
		err := plugins.ValidateNamespaceOwnership("fli", "io.example.flights", lock, plugins.NamespaceOptions{})
		if err != nil {
			t.Fatalf("matching owner re-install must succeed: %v", err)
		}
	})

	t.Run("mismatched owner rejected without override", func(t *testing.T) {
		t.Parallel()
		lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")
		err := plugins.ValidateNamespaceOwnership("fli", "io.attacker.flights", lock, plugins.NamespaceOptions{})
		if err == nil || !errors.Is(err, plugins.ErrPluginNamespaceConflict) {
			t.Fatalf("mismatched owner must return PLUGIN_NAMESPACE_CONFLICT; got %v", err)
		}
	})

	t.Run("missing namespace_owner declaration rejected", func(t *testing.T) {
		t.Parallel()
		lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		err := plugins.ValidateNamespaceOwnership("fli", "", lock, plugins.NamespaceOptions{})
		if err == nil || !errors.Is(err, plugins.ErrPluginNamespaceConflict) {
			t.Fatalf("empty namespace_owner must return PLUGIN_NAMESPACE_CONFLICT; got %v", err)
		}
	})

	t.Run("dev-allow-namespace-conflict bypasses on dev profile", func(t *testing.T) {
		t.Parallel()
		lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")
		opts := plugins.NamespaceOptions{ProfileIsDev: true, AllowConflictOverride: true}
		if err := plugins.ValidateNamespaceOwnership("fli", "io.attacker.flights", lock, opts); err != nil {
			t.Fatalf("dev override must permit mismatched owner; got %v", err)
		}
	})

	t.Run("dev-allow-namespace-conflict rejected on non-dev profile", func(t *testing.T) {
		t.Parallel()
		lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")
		opts := plugins.NamespaceOptions{ProfileIsDev: false, AllowConflictOverride: true}
		err := plugins.ValidateNamespaceOwnership("fli", "io.attacker.flights", lock, opts)
		if err == nil || !errors.Is(err, plugins.ErrPluginNamespaceConflict) {
			t.Fatalf("override outside dev profile must still fail; got %v", err)
		}
	})

	t.Run("override flag alone (no dev profile) does not bypass", func(t *testing.T) {
		t.Parallel()
		lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		plugins.RecordNamespaceOwner(lock, "fli", "io.example.flights")
		opts := plugins.NamespaceOptions{ProfileIsDev: true, AllowConflictOverride: false}
		err := plugins.ValidateNamespaceOwnership("fli", "io.attacker.flights", lock, opts)
		if err == nil || !errors.Is(err, plugins.ErrPluginNamespaceConflict) {
			t.Fatalf("dev profile without explicit override flag must still fail; got %v", err)
		}
	})

	t.Run("cross-profile locks never merge", func(t *testing.T) {
		t.Parallel()
		// Two profiles = two separate PluginsLock instances. Locking "fli" to
		// owner A in profile-1's lock MUST NOT affect profile-2's lock.
		profile1 := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		profile2 := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		plugins.RecordNamespaceOwner(profile1, "fli", "io.example.flights")
		if _, found := plugins.LookupNamespaceOwner(profile2, "fli"); found {
			t.Fatalf("profile2 lock must not see profile1's prefix binding")
		}
		// A different owner installing the same prefix on profile-2 must
		// succeed because profile-2 has its own independent state.
		if err := plugins.ValidateNamespaceOwnership("fli", "io.competitor.flights", profile2, plugins.NamespaceOptions{}); err != nil {
			t.Fatalf("profile2 first install must succeed despite profile1 having prefix: %v", err)
		}
	})

	t.Run("RecordNamespaceOwner updates existing entry in place", func(t *testing.T) {
		t.Parallel()
		// Used by `gum plugin transfer-namespace` per spec §5.1 transfer
		// procedure. The row is updated rather than duplicated.
		lock := &catalog.PluginsLock{PluginsLockSchemaVersion: 1}
		plugins.RecordNamespaceOwner(lock, "fli", "old.owner")
		plugins.RecordNamespaceOwner(lock, "fli", "new.owner")
		if len(lock.Plugins) != 1 {
			t.Fatalf("expected exactly one row for prefix after re-record; got %d", len(lock.Plugins))
		}
		owner, _ := plugins.LookupNamespaceOwner(lock, "fli")
		if owner != "new.owner" {
			t.Fatalf("expected updated owner=new.owner; got %q", owner)
		}
	})
}
