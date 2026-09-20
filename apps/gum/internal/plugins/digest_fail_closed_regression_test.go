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

// TestHostStartRefusesMissingDigestSidecar pins the fail-closed fix. A missing
// sidecar used to return ("", nil), which made Start skip
// VerifyExecutableBinding entirely: deleting one file next to the binary was
// enough to spawn a swapped executable with no error.
func TestHostStartRefusesMissingDigestSidecar(t *testing.T) {
	installRoot := t.TempDir()
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})

	id, err := host.Install(context.Background(), filepath.Join(testdataDir(), "namespaced-plugin"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	sidecar := filepath.Join(installRoot, id, ".executable.sha256")
	if err := os.Remove(sidecar); err != nil {
		t.Fatalf("remove sidecar: %v", err)
	}
	// Swap the binary too, so a fail-open Start spawns foreign code.
	execPath := filepath.Join(installRoot, id, "executable")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\necho swapped\n"), 0o755); err != nil {
		t.Fatalf("swap binary: %v", err)
	}

	plug, err := host.Start(context.Background(), id)
	if plug != nil {
		_ = plug.Stop(context.Background())
	}
	if !errors.Is(err, plugins.ErrExecutableUntrusted) {
		t.Errorf("Start err = %v; want ErrExecutableUntrusted", err)
	}
}

// TestDigestSidecarIsOwnerOnly pins the 0o600 mode on both install paths. At
// 0o644 any local account could read the recorded digest, and on a group- or
// world-writable install root, rewrite it to match a swapped binary.
func TestDigestSidecarIsOwnerOnly(t *testing.T) {
	src := filepath.Join(testdataDir(), "namespaced-plugin")

	t.Run("legacy install", func(t *testing.T) {
		installRoot := t.TempDir()
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
		id, err := host.Install(context.Background(), src)
		if err != nil {
			t.Fatalf("Install: %v", err)
		}
		assertSidecarMode(t, filepath.Join(installRoot, id, ".executable.sha256"))
	})

	t.Run("registry install", func(t *testing.T) {
		installRoot := t.TempDir()
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
		id, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
			Registry: registry.New(t.TempDir()),
		})
		if err != nil {
			t.Fatalf("InstallWithRegistry: %v", err)
		}
		assertSidecarMode(t, filepath.Join(installRoot, id, ".executable.sha256"))
	})
}

func assertSidecarMode(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat sidecar: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("sidecar mode = %#o; want 0600", got)
	}
}

// TestHostStartRejectsDigestDisagreement pins HostConfig.TrustedDigest: when
// the recorded registry digest and the sidecar disagree, one of the two was
// rewritten after install, so Start must refuse instead of trusting either.
func TestHostStartRejectsDigestDisagreement(t *testing.T) {
	installRoot := t.TempDir()
	const recorded = "0000000000000000000000000000000000000000000000000000000000000000"
	host := plugins.NewHost(plugins.HostConfig{
		InstallRoot:   installRoot,
		TrustedDigest: func(string) (string, error) { return recorded, nil },
	})

	id, err := host.Install(context.Background(), filepath.Join(testdataDir(), "namespaced-plugin"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	plug, err := host.Start(context.Background(), id)
	if plug != nil {
		_ = plug.Stop(context.Background())
	}
	if !errors.Is(err, plugins.ErrExecutableUntrusted) {
		t.Errorf("Start err = %v; want ErrExecutableUntrusted", err)
	}
}

// TestHostStartAcceptsAgreeingRecordedDigest keeps the TrustedDigest hook from
// breaking the normal path: an agreeing registry digest clears the trust gate.
// The testdata executable is not a real MCP server, so the spawn still fails at
// the initialize handshake; the assertion is that it is not a trust failure.
func TestHostStartAcceptsAgreeingRecordedDigest(t *testing.T) {
	installRoot := t.TempDir()
	var sidecarDigest string
	host := plugins.NewHost(plugins.HostConfig{
		InstallRoot:   installRoot,
		TrustedDigest: func(string) (string, error) { return sidecarDigest, nil },
	})

	id, err := host.Install(context.Background(), filepath.Join(testdataDir(), "namespaced-plugin"))
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(installRoot, id, ".executable.sha256"))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	sidecarDigest = string(raw[:64])

	plug, err := host.Start(context.Background(), id)
	if plug != nil {
		_ = plug.Stop(context.Background())
	}
	if errors.Is(err, plugins.ErrExecutableUntrusted) {
		t.Errorf("Start err = %v; want the trust gate to pass", err)
	}
}

// TestRecordedDigestResolverReadsLockRow pins the production wiring: the
// resolver returns the plugins.lock executable_sha256 for an installed plugin
// and "" for an unknown one, so a legacy install with no lock row still falls
// back to its sidecar instead of failing.
func TestRecordedDigestResolverReadsLockRow(t *testing.T) {
	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})

	id, err := host.InstallWithRegistry(context.Background(), filepath.Join(testdataDir(), "namespaced-plugin"),
		plugins.InstallOptions{Registry: reg})
	if err != nil {
		t.Fatalf("InstallWithRegistry: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(installRoot, id, ".executable.sha256"))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	want := string(raw[:64])

	resolve := plugins.RecordedDigestResolver(reg)
	got, err := resolve(id)
	if err != nil {
		t.Fatalf("resolve(%q): %v", id, err)
	}
	if got != want {
		t.Errorf("resolve(%q) = %q; want %q", id, got, want)
	}

	got, err = resolve("no.such.plugin")
	if err != nil {
		t.Fatalf("resolve(unknown): %v", err)
	}
	if got != "" {
		t.Errorf("resolve(unknown) = %q; want \"\"", got)
	}

	if plugins.RecordedDigestResolver(nil) != nil {
		t.Error("RecordedDigestResolver(nil) returned a non-nil resolver")
	}
}
