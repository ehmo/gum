// Spec §8.7 line 1893: `gum plugin remove` drops the plugin's registry
// entries under the same transaction protocol install uses. The CLI must
// therefore route remove through RemoveWithRegistry whenever a profile exists,
// and degrade to the file-only Host.Remove only when there is no registry to
// write — the same degradation the install subcommand makes.

package main_test

import (
	"context"
	"testing"

	gummain "github.com/ehmo/gum/cmd/gum"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

func TestPluginRemoveUsesRegistryWhenProfileExists(t *testing.T) {
	dir := t.TempDir()
	factory := gummain.PluginRegistryFactory(func(string) *registry.Registry { return registry.New(dir) })

	var gotID string
	var gotReg *registry.Registry
	host := &mockHost{
		removeFn: func(context.Context, string) error {
			t.Fatal("remove fell back to the file-only Host.Remove while a profile exists")
			return nil
		},
		removeWithRegistryFn: func(_ context.Context, pluginID string, opts plugins.RemoveOptions) error {
			gotID = pluginID
			gotReg = opts.Registry
			return nil
		},
	}

	out, err := gummain.DispatchPluginCommandWithRegistry([]string{"remove", "google-flights"}, host, dir, factory)
	if err != nil {
		t.Fatalf("plugin remove: %v", err)
	}
	if gotID != "google-flights" {
		t.Errorf("RemoveWithRegistry got plugin id %q; want google-flights", gotID)
	}
	if gotReg == nil {
		t.Error("RemoveWithRegistry got a nil registry; the transaction cannot run")
	}
	if out != "removed google-flights\n" {
		t.Errorf("output = %q; want %q", out, "removed google-flights\n")
	}
}

func TestPluginRemoveWithoutProfileStaysFileOnly(t *testing.T) {
	var called bool
	host := &mockHost{
		removeFn: func(_ context.Context, pluginID string) error {
			called = true
			if pluginID != "google-flights" {
				t.Errorf("Remove got %q; want google-flights", pluginID)
			}
			return nil
		},
		removeWithRegistryFn: func(context.Context, string, plugins.RemoveOptions) error {
			t.Fatal("remove opened a registry transaction with no profile dir")
			return nil
		},
	}

	if _, err := gummain.DispatchPluginCommand([]string{"remove", "google-flights"}, host); err != nil {
		t.Fatalf("plugin remove: %v", err)
	}
	if !called {
		t.Error("Host.Remove was never called")
	}
}
