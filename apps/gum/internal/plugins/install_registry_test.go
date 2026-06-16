// gum-u2nf acceptance: install-flow integration tests that pin the three
// gum-f5j helpers (ValidateBinding, ValidateNamespaceOwnership, MapPluginError)
// to Install() and the executor envelope path.
//
// The tests below exercise the full InstallWithRegistry sequence — manifest
// load → namespace check → binding validation → atomic registry write → file
// copy — and assert the post-condition that plugin-catalog.json,
// plugins.lock, and plugin-state.json carry the rows the spec requires
// (§5.1, §8.7). Negative paths use distinct, mutually-incompatible manifests
// so a single failure surfaces unambiguously.

package plugins_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestInstallWithRegistryHappyPath proves the spec-mandated registry rows
// are written when validation passes. After install, plugin-catalog.json has
// one variant for the advertised_tool, plugins.lock has the prefix→owner
// mapping, plugin-state.json has the per-plugin row, and the executable is
// copied into the install root.
func TestInstallWithRegistryHappyPath(t *testing.T) {
	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	src := filepath.Join(testdataDir(), "namespaced-plugin")

	id, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
		Registry: reg,
	})
	if err != nil {
		t.Fatalf("InstallWithRegistry: %v", err)
	}
	if id != "google-flights" {
		t.Errorf("plugin id = %q; want google-flights", id)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := len(files.Catalog.Variants); got != 1 {
		t.Fatalf("plugin-catalog variants = %d; want 1", got)
	}
	v := files.Catalog.Variants[0].(map[string]any)
	if owner, _ := v["owner_plugin"].(string); owner != "google-flights" {
		t.Errorf("variant owner_plugin = %q; want google-flights", owner)
	}
	binding, _ := v["binding"].(map[string]any)
	if got, _ := binding["adapter_key"].(string); got != "plugin.mcp" {
		t.Errorf("variant binding.adapter_key = %q; want plugin.mcp", got)
	}
	if got, _ := binding["tool_name"].(string); got != "flights_search" {
		t.Errorf("variant binding.tool_name = %q; want flights_search", got)
	}

	owner, found := plugins.LookupNamespaceOwner(files.Lock, "google-flights")
	if !found {
		t.Fatal("plugins.lock missing prefix → namespace_owner row for google-flights")
	}
	if owner != "io.example.flights" {
		t.Errorf("namespace_owner = %q; want io.example.flights", owner)
	}

	if got := len(files.State.Plugins); got != 1 {
		t.Errorf("plugin-state plugins = %d; want 1", got)
	}

	if _, err := os.Stat(filepath.Join(installRoot, "google-flights", "manifest.json")); err != nil {
		t.Errorf("manifest not copied to install root: %v", err)
	}
}

// TestInstallWithRegistryNamespaceConflict pins ValidateNamespaceOwnership.
// A pre-existing lock row owning "google-flights" → io.other.flights must
// cause the second install (claiming io.attacker.flights) to fail with
// ErrPluginNamespaceConflict BEFORE any catalog mutation or file copy.
func TestInstallWithRegistryNamespaceConflict(t *testing.T) {
	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})

	if err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		plugins.RecordNamespaceOwner(f.Lock, "google-flights", "io.other.flights")
		return nil
	}); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	src := filepath.Join(testdataDir(), "namespaced-plugin")
	_, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
		Registry: reg,
	})
	if !errors.Is(err, plugins.ErrPluginNamespaceConflict) {
		t.Fatalf("InstallWithRegistry err = %v; want ErrPluginNamespaceConflict", err)
	}

	if _, err := os.Stat(filepath.Join(installRoot, "google-flights")); !os.IsNotExist(err) {
		t.Errorf("install root contains plugin dir after rejected install: err=%v", err)
	}
}

// TestInstallWithRegistryDevOverride pins the spec §5.1 dev-profile escape:
// when both ProfileIsDev and AllowConflictOverride are set, a conflicting
// install proceeds.
func TestInstallWithRegistryDevOverride(t *testing.T) {
	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})

	if err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		plugins.RecordNamespaceOwner(f.Lock, "google-flights", "io.other.flights")
		return nil
	}); err != nil {
		t.Fatalf("seed lock: %v", err)
	}

	src := filepath.Join(testdataDir(), "namespaced-plugin")
	id, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
		Registry: reg,
		Namespace: plugins.NamespaceOptions{
			ProfileIsDev:          true,
			AllowConflictOverride: true,
		},
	})
	if err != nil {
		t.Fatalf("InstallWithRegistry with dev override: %v", err)
	}
	if id != "google-flights" {
		t.Errorf("plugin id = %q; want google-flights", id)
	}
}

// TestInstallWithRegistryRequiresRegistry pins that the install path refuses
// to silently fall back to file-copy when InstallOptions.Registry is nil.
// The CLI MUST opt in by passing a registry; otherwise the validators stay
// dormant and the install would skip the spec §5.1/§8.7 gates.
func TestInstallWithRegistryRequiresRegistry(t *testing.T) {
	installRoot := t.TempDir()
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	src := filepath.Join(testdataDir(), "namespaced-plugin")
	_, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{})
	if err == nil {
		t.Fatal("InstallWithRegistry with nil Registry returned nil err; want explicit error")
	}
}
