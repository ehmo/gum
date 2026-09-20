// Spec §8.7 line 1893: "`gum plugin remove` MUST remove the corresponding
// entries under the same full-state transaction protocol."
//
// Host.Remove is os.RemoveAll of the install directory and nothing else, so a
// removed plugin keeps its plugin-catalog.json variants, its plugins.lock row
// and namespace lease, and its plugin-state.json quarantine flag. The two
// consequences the tests below pin:
//
//   - a replacement plugin from a different vendor is refused the namespace,
//     because the lock still names the removed plugin's owner;
//   - a reinstall inherits the removed plugin's quarantine row.

package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestRemoveWithRegistryClearsAllThreeFiles is the §8.7 remove contract: after
// remove, none of the three registry files mentions the plugin and the install
// directory is gone.
func TestRemoveWithRegistryClearsAllThreeFiles(t *testing.T) {
	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})

	id, err := host.InstallWithRegistry(context.Background(), filepath.Join(testdataDir(), "namespaced-plugin"),
		plugins.InstallOptions{Registry: reg})
	if err != nil {
		t.Fatalf("InstallWithRegistry: %v", err)
	}

	// Quarantine the plugin first: a removed plugin's quarantine row is the
	// state a reinstall must not inherit.
	if err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		for _, raw := range f.State.Plugins {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if name, _ := row["name"].(string); name == id {
				row["quarantined"] = true
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("quarantine setup: %v", err)
	}

	if err := host.RemoveWithRegistry(context.Background(), id, plugins.RemoveOptions{Registry: reg}); err != nil {
		t.Fatalf("RemoveWithRegistry: %v", err)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := len(files.Catalog.Variants); got != 0 {
		t.Errorf("plugin-catalog variants = %d after remove; want 0", got)
	}
	if got := len(files.Lock.Plugins); got != 0 {
		t.Errorf("plugins.lock plugins = %d after remove; want 0", got)
	}
	if got := len(files.State.Plugins); got != 0 {
		t.Errorf("plugin-state plugins = %d after remove; want 0", got)
	}
	if owner, found := plugins.LookupNamespaceOwner(files.Lock, id); found {
		t.Errorf("plugins.lock still leases namespace %q to %q after remove", id, owner)
	}
	if _, err := os.Stat(filepath.Join(installRoot, id)); !os.IsNotExist(err) {
		t.Errorf("install dir still present after remove: stat err = %v", err)
	}
}

// TestRemoveThenInstallDifferentOwnerSucceeds is the operator-visible
// consequence: after remove, a different vendor may claim the namespace.
func TestRemoveThenInstallDifferentOwnerSucceeds(t *testing.T) {
	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	ctx := context.Background()

	src := filepath.Join(testdataDir(), "namespaced-plugin")
	id, err := host.InstallWithRegistry(ctx, src, plugins.InstallOptions{Registry: reg})
	if err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := host.RemoveWithRegistry(ctx, id, plugins.RemoveOptions{Registry: reg}); err != nil {
		t.Fatalf("RemoveWithRegistry: %v", err)
	}

	// Same plugin_id, different namespace_owner: this is exactly the case
	// ValidateNamespaceOwnership refuses while the stale lease survives.
	replacement := copyPluginSource(t, src, map[string]string{
		`"namespace_owner": "io.example.flights"`: `"namespace_owner": "com.other.flights"`,
	})
	if _, err := host.InstallWithRegistry(ctx, replacement, plugins.InstallOptions{Registry: reg}); err != nil {
		t.Fatalf("reinstall under a new namespace_owner after remove: %v", err)
	}
}

// copyPluginSource copies a testdata plugin dir into a fresh tempdir, applying
// literal substitutions to manifest.json. Returns the new source dir.
func copyPluginSource(t *testing.T, src string, manifestEdits map[string]string) string {
	t.Helper()
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read plugin source: %v", err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		mode := os.FileMode(0o644)
		if e.Name() == "manifest.json" {
			for old, next := range manifestEdits {
				if !strings.Contains(string(b), old) {
					t.Fatalf("manifest edit %q not found in %s", old, src)
				}
				b = []byte(strings.ReplaceAll(string(b), old, next))
			}
		} else {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, mode); err != nil {
			t.Fatalf("write %s: %v", e.Name(), err)
		}
	}
	return dst
}
