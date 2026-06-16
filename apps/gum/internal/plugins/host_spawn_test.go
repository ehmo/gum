// Spec §8.7 line 1690: plugin spawn MUST re-verify the installed
// executable's sha256 against the binding captured at install time. A
// mutated-binary-on-disk attack must surface as PLUGIN_EXECUTABLE_UNTRUSTED
// before any subprocess is exec'd.
//
// TDD red (gum-46uq): host.Start currently does NOT call
// VerifyExecutableBinding, so this test fails until gum-25xk lands.

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

// TestHostStartRejectsTamperedBinary installs a plugin, mutates the binary
// on disk, then calls Start and expects PLUGIN_EXECUTABLE_UNTRUSTED.
func TestHostStartRejectsTamperedBinary(t *testing.T) {
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

	// Mutate the installed binary so its sha256 no longer matches the
	// digest captured at install time.
	execPath := filepath.Join(installRoot, id, "executable")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\necho 'tampered'\n"), 0o755); err != nil {
		t.Fatalf("mutate binary: %v", err)
	}

	plug, err := host.Start(context.Background(), id)
	if plug != nil {
		// Stop the rogue subprocess if Start (incorrectly) succeeded.
		_ = plug.Stop(context.Background())
	}
	if err == nil {
		t.Fatal("host.Start returned nil error after binary tampering; want PLUGIN_EXECUTABLE_UNTRUSTED")
	}
	if !errors.Is(err, plugins.ErrExecutableUntrusted) {
		t.Errorf("host.Start err = %v; want errors.Is(err, ErrExecutableUntrusted)", err)
	}
}

// TestHostStartRejectsTamperedBinaryLegacyInstall pins gum-62ph: a plugin
// installed via the LEGACY Host.Install() path (no registry) now also gets a
// digest sidecar, so Start() rejects a tampered binary instead of silently
// skipping verification (the bypass the audit found).
func TestHostStartRejectsTamperedBinaryLegacyInstall(t *testing.T) {
	installRoot := t.TempDir()
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})

	src := filepath.Join(testdataDir(), "namespaced-plugin")
	id, err := host.Install(context.Background(), src)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	// The fix: legacy Install must now write the digest sidecar.
	if _, err := os.Stat(filepath.Join(installRoot, id, ".executable.sha256")); err != nil {
		t.Fatalf("legacy Install did not write the digest sidecar: %v", err)
	}
	// Tamper the installed binary; Start must reject it before spawning.
	execPath := filepath.Join(installRoot, id, "executable")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\necho 'tampered'\n"), 0o755); err != nil {
		t.Fatalf("mutate binary: %v", err)
	}
	plug, err := host.Start(context.Background(), id)
	if plug != nil {
		_ = plug.Stop(context.Background())
	}
	if !errors.Is(err, plugins.ErrExecutableUntrusted) {
		t.Errorf("host.Start err = %v; want ErrExecutableUntrusted (legacy install must be verifiable)", err)
	}
}
