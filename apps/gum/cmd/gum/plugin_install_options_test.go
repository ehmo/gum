// gum-u2nf acceptance: install-subcommand → InstallWithRegistry plumbing.
//
// Tests pin the CLI-side wiring of --dev-allow-namespace-conflict and
// ProfileIsDev: when profile_dir is set, DispatchPluginCommandWithOptions
// routes "install <path>" through host.InstallWithRegistry, carrying the
// resolved namespace options. Without this wiring the install command
// would silently skip the spec §5.1/§8.7 validators on production.

package main_test

import (
	"context"
	"testing"

	gummain "github.com/ehmo/gum/cmd/gum"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestPluginInstallRoutesThroughRegistry pins that when profileDir is
// supplied, the install subcommand uses InstallWithRegistry — the path that
// runs ValidateBinding + ValidateNamespaceOwnership before writing the
// catalog/lock/state files.
func TestPluginInstallRoutesThroughRegistry(t *testing.T) {
	var gotOpts plugins.InstallOptions
	var gotSource string
	host := &mockHost{
		installWithRegistryFn: func(_ context.Context, source string, opts plugins.InstallOptions) (string, error) {
			gotSource = source
			gotOpts = opts
			return "google-flights", nil
		},
	}
	profileDir := t.TempDir()
	regFactory := func(string) *registry.Registry { return registry.New(profileDir) }

	out, err := gummain.DispatchPluginCommandWithOptions(
		[]string{"install", "/some/path"},
		host,
		profileDir,
		regFactory,
		gummain.PluginInstallOptions{
			ProfileIsDev:           true,
			AllowNamespaceConflict: true,
		},
	)
	if err != nil {
		t.Fatalf("DispatchPluginCommandWithOptions: %v", err)
	}
	if gotSource != "/some/path" {
		t.Errorf("InstallWithRegistry source = %q; want /some/path", gotSource)
	}
	if gotOpts.Registry == nil {
		t.Error("InstallWithRegistry called with nil Registry; CLI must inject the profile's registry")
	}
	if !gotOpts.Namespace.ProfileIsDev {
		t.Error("Namespace.ProfileIsDev = false; CLI did not plumb ProfileIsDev flag")
	}
	if !gotOpts.Namespace.AllowConflictOverride {
		t.Error("Namespace.AllowConflictOverride = false; CLI did not plumb --dev-allow-namespace-conflict flag")
	}
	if out == "" {
		t.Error("dispatch returned empty result on successful install")
	}
}

// TestPluginInstallFallbackWithoutProfile preserves the legacy file-copy
// path: when profileDir is empty, the install subcommand falls back to
// host.Install (no registry mutation). Existing callers that haven't been
// upgraded keep working without surprise.
func TestPluginInstallFallbackWithoutProfile(t *testing.T) {
	var registryCalls int
	var legacyCalls int
	host := &mockHost{
		installWithRegistryFn: func(_ context.Context, _ string, _ plugins.InstallOptions) (string, error) {
			registryCalls++
			return "x", nil
		},
		installFn: func(_ context.Context, _ string) (string, error) {
			legacyCalls++
			return "x", nil
		},
	}
	_, err := gummain.DispatchPluginCommandWithOptions(
		[]string{"install", "/p"},
		host,
		"", // no profile dir → legacy path
		nil,
		gummain.PluginInstallOptions{},
	)
	if err != nil {
		t.Fatalf("dispatch err: %v", err)
	}
	if registryCalls != 0 {
		t.Errorf("InstallWithRegistry calls = %d; want 0 (no profile dir)", registryCalls)
	}
	if legacyCalls != 1 {
		t.Errorf("Install calls = %d; want 1 (legacy fallback)", legacyCalls)
	}
}
