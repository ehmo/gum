// Spec gum-1ugz: plugin install MUST normalise filesystem modes on every
// copied artifact, regardless of the source bits:
//
//   - executable file:  0o755
//   - manifest.json:    0o644
//   - install directory: 0o755
//
// Source bits like setuid (0o4000), setgid (0o2000), and world-writable
// (0o0002) must be stripped — they are common attack surface and must
// not be inherited verbatim from the install source.
//
// TDD red (gum-46uq): plugins.Install copies files with os.OpenFile +
// io.Copy using the source mode (or the open-mode default 0o644), so a
// 0o4777 source binary lands as a 0o4777 installed binary. This test
// fails until gum-1ugz lands.

package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestInstallNormalisesFileModes builds a synthetic plugin source dir with
// hostile mode bits (setuid, setgid, world-writable) and asserts every
// installed artifact lands with the spec-mandated mode regardless of umask.
// The umask is zeroed for the duration of the test so that the install
// code — not umask — is what strips the bits.
func TestInstallNormalisesFileModes(t *testing.T) {
	prevUmask := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(prevUmask) })

	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})

	// Build a fresh source dir we own so chmod() takes effect. Reuses the
	// namespaced-plugin shape so the manifest validates.
	srcDir := t.TempDir()
	manifest := []byte(`{
  "manifest_schema_version": 1,
  "plugin_id": "google-flights",
  "name": "Google Flights",
  "version": "0.1.0",
  "namespace_owner": "io.example.flights",
  "shape": "mcp-plugin",
  "executable": "executable",
  "advertised_tools": [
    {
      "name": "flights_search",
      "description": "Search Google Flights itineraries",
      "risk_class": "read"
    }
  ],
  "declared_capabilities": {
    "network": true,
    "fs_write_dir": "",
    "env_allow": []
  }
}
`)
	if err := os.WriteFile(filepath.Join(srcDir, "manifest.json"), manifest, 0o666); err != nil {
		t.Fatalf("write source manifest: %v", err)
	}
	execSrc := filepath.Join(srcDir, "executable")
	if err := os.WriteFile(execSrc, []byte("#!/bin/sh\necho fake-plugin\n"), 0o755); err != nil {
		t.Fatalf("write source executable: %v", err)
	}
	// Source has setuid + setgid + world-writable. ALL must be stripped
	// by Install. Numeric 0o4000/0o2000 do not set setuid/setgid via the
	// Go API — those bits live in the high-bit os.ModeSetuid/ModeSetgid
	// flags. Combine with 0o777 for the perm bits.
	if err := os.Chmod(execSrc, os.ModeSetuid|os.ModeSetgid|0o777); err != nil {
		t.Fatalf("chmod source executable: %v", err)
	}

	id, err := host.InstallWithRegistry(context.Background(), srcDir, plugins.InstallOptions{
		Registry: reg,
	})
	if err != nil {
		t.Fatalf("InstallWithRegistry: %v", err)
	}

	installDir := filepath.Join(installRoot, id)
	dirInfo, err := os.Stat(installDir)
	if err != nil {
		t.Fatalf("stat install dir: %v", err)
	}
	t.Logf("install dir mode = %o", dirInfo.Mode().Perm())
	if got := dirInfo.Mode().Perm(); got != 0o755 {
		t.Errorf("install dir mode = %o; want 0o755", got)
	}

	manifestInfo, err := os.Stat(filepath.Join(installDir, "manifest.json"))
	if err != nil {
		t.Fatalf("stat installed manifest: %v", err)
	}
	if got := manifestInfo.Mode().Perm(); got != 0o644 {
		t.Errorf("installed manifest.json mode = %o; want 0o644", got)
	}

	execInfo, err := os.Stat(filepath.Join(installDir, "executable"))
	if err != nil {
		t.Fatalf("stat installed executable: %v", err)
	}
	t.Logf("installed executable mode = %o (full=%o)", execInfo.Mode().Perm(), uint32(execInfo.Mode()))
	if got := execInfo.Mode().Perm(); got != 0o755 {
		t.Errorf("installed executable mode = %o; want 0o755 (setuid/world-writable bits must be stripped)", got)
	}
	// setuid (os.ModeSetuid) and setgid (os.ModeSetgid) must be cleared.
	if execInfo.Mode()&os.ModeSetuid != 0 {
		t.Errorf("installed executable still has setuid bit set; install MUST strip it")
	}
	if execInfo.Mode()&os.ModeSetgid != 0 {
		t.Errorf("installed executable still has setgid bit set; install MUST strip it")
	}
}
