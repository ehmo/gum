package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// installFakePlugin writes a fake plugin executable on disk and returns the
// binding the host would record at install time. The body argument controls
// the exact bytes hashed so tests can later tamper.
func installFakePlugin(t *testing.T, installRoot, name string, body []byte) *ExecutableBinding {
	t.Helper()
	binDir := filepath.Join(installRoot, name, "venv", "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", binDir, err)
	}
	binPath := filepath.Join(binDir, name)
	if err := os.WriteFile(binPath, body, 0o700); err != nil {
		t.Fatalf("write %s: %v", binPath, err)
	}
	sum := sha256.Sum256(body)
	return &ExecutableBinding{
		Name:             name,
		InstallRoot:      filepath.Join(installRoot, name),
		ExecutablePath:   binPath,
		ExecutableSHA256: hex.EncodeToString(sum[:]),
		ArgvNormalized:   []string{binPath, "mcp"},
	}
}

// TestPluginExecutableBinding is the spec §8.7 line 1690 acceptance test:
// after install, the binding verifies; after the executable is tampered, the
// re-hash must fail with PLUGIN_EXECUTABLE_UNTRUSTED and the host MUST
// quarantine the plugin in plugin-state.json.
func TestPluginExecutableBinding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binding := installFakePlugin(t, root, "google-flights", []byte("#!/bin/sh\necho fake-mcp\n"))

	// Happy path: the digest matches; spawn-authorisation succeeds.
	if err := VerifyExecutableBinding(binding); err != nil {
		t.Fatalf("VerifyExecutableBinding (clean install): %v", err)
	}

	// Tamper: rewrite the executable; re-check must fail with
	// PLUGIN_EXECUTABLE_UNTRUSTED.
	if err := os.WriteFile(binding.ExecutablePath, []byte("malicious!"), 0o700); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	err := VerifyExecutableBinding(binding)
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Fatalf("post-tamper Verify err = %v; want ErrExecutableUntrusted", err)
	}

	// The host MUST quarantine the plugin once the digest disagrees. Boot a
	// registry, seed the state row, then call QuarantinePlugin.
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	if err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		f.State.Plugins = append(f.State.Plugins, map[string]any{
			"name":            "google-flights",
			"installed_at":    "2026-05-19T00:00:00Z",
			"activated_at":    nil,
			"quarantined":     false,
			"last_error_code": nil,
		})
		f.Lock.Plugins = append(f.Lock.Plugins, map[string]any{
			"name":              "google-flights",
			"version":           "1.2.0",
			"executable_sha256": binding.ExecutableSHA256,
		})
		return nil
	}); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	if err := QuarantinePlugin(context.Background(), reg, "google-flights", "PLUGIN_EXECUTABLE_UNTRUSTED"); err != nil {
		t.Fatalf("QuarantinePlugin: %v", err)
	}
	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	row, ok := files.State.Plugins[0].(map[string]any)
	if !ok {
		t.Fatalf("state row not a map: %T", files.State.Plugins[0])
	}
	if v, _ := row["quarantined"].(bool); !v {
		t.Errorf("quarantined = %v; want true", row["quarantined"])
	}
	if s, _ := row["last_error_code"].(string); s != "PLUGIN_EXECUTABLE_UNTRUSTED" {
		t.Errorf("last_error_code = %v; want PLUGIN_EXECUTABLE_UNTRUSTED", row["last_error_code"])
	}
	if s, _ := row["quarantined_at"].(string); s == "" {
		t.Errorf("quarantined_at empty; want RFC 3339 timestamp")
	}
}

// TestVerifyRejectsShellInterpreters proves the spec §8.7 line 1690 deny-list:
// even if the digest matches, a binding that points executable_path at
// /bin/sh (or any other interpreter) MUST be rejected. This is the safeguard
// against a manifest that smuggles `command=["sh","-c","..."]` past install.
func TestVerifyRejectsShellInterpreters(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binDir := filepath.Join(root, "naughty", "venv", "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	shPath := filepath.Join(binDir, "sh")
	if err := os.WriteFile(shPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write fake sh: %v", err)
	}
	sum := sha256.Sum256([]byte("#!/bin/sh\n"))
	b := &ExecutableBinding{
		Name:             "naughty",
		InstallRoot:      filepath.Join(root, "naughty"),
		ExecutablePath:   shPath,
		ExecutableSHA256: hex.EncodeToString(sum[:]),
	}
	err := VerifyExecutableBinding(b)
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Fatalf("Verify(sh path) = %v; want ErrExecutableUntrusted", err)
	}
}

// TestVerifyRejectsRelativePath and TestVerifyRejectsInstallRootEscape together
// cover the two non-digest containment checks called out in spec §8.7
// line 1690: "executable_path MUST be absolute and inside the host-managed
// install root".
func TestVerifyRejectsRelativePath(t *testing.T) {
	t.Parallel()
	b := &ExecutableBinding{
		Name:             "x",
		InstallRoot:      "/abs/install/root",
		ExecutablePath:   "relative/bin/x",
		ExecutableSHA256: "deadbeef",
	}
	err := VerifyExecutableBinding(b)
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Errorf("Verify(relative) = %v; want ErrExecutableUntrusted", err)
	}
}

func TestVerifyRejectsInstallRootEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outsideDir := filepath.Join(root, "outside")
	if err := os.MkdirAll(outsideDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	bin := filepath.Join(outsideDir, "tool")
	if err := os.WriteFile(bin, []byte("payload"), 0o700); err != nil {
		t.Fatalf("write: %v", err)
	}
	sum := sha256.Sum256([]byte("payload"))
	insideRoot := filepath.Join(root, "tool-install")
	if err := os.MkdirAll(insideRoot, 0o700); err != nil {
		t.Fatalf("mkdir install: %v", err)
	}
	b := &ExecutableBinding{
		Name:             "tool",
		InstallRoot:      insideRoot,
		ExecutablePath:   bin, // lives in sibling dir, not inside insideRoot
		ExecutableSHA256: hex.EncodeToString(sum[:]),
	}
	err := VerifyExecutableBinding(b)
	if !errors.Is(err, ErrExecutableUntrusted) {
		t.Errorf("Verify(escape) = %v; want ErrExecutableUntrusted", err)
	}
}

// TestVerifyAcceptsPrefixedDigest covers the equalDigest tolerance for
// plugins.lock entries that record "sha256:<hex>" (PyPI tooling default) vs
// bare hex (Go ecosystem default). Both must be accepted.
func TestVerifyAcceptsPrefixedDigest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binding := installFakePlugin(t, root, "prefixed", []byte("payload"))
	binding.ExecutableSHA256 = "sha256:" + binding.ExecutableSHA256
	if err := VerifyExecutableBinding(binding); err != nil {
		t.Errorf("Verify with sha256:-prefixed digest: %v", err)
	}
}
