package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// writePluginSource writes a minimal, valid zero-credential plugin
// (manifest.json + placeholder executable) directly into dir. The shape
// mirrors the manifest the host validates on install/setup; with no
// credential_descriptors, setup completes without prompting or a canary.
func writePluginSource(t *testing.T, dir, pluginID string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir plugin source: %v", err)
	}
	m := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    "Test Plugin",
		"version":                 "0.1.0",
		"shape":                   "mcp-plugin",
		"executable":              "executable",
		"namespace_owner":         "owner-a",
		"advertised_tools": []map[string]any{
			{"name": "echo", "description": "echo", "risk_class": "read"},
		},
		"declared_capabilities": map[string]any{
			"network":      false,
			"fs_write_dir": "",
			"env_allow":    []string{},
		},
		"requirements": map[string]any{
			"needs_user_creds":       []string{},
			"credential_descriptors": []any{},
		},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "executable"), nil, 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
}

// The cobra RunE closures on the plugin subcommands are thin glue:
// resolveProfileFlag → resolveProfileDir → DispatchPluginCommand* → print.
// The shape tests pin the static wiring and the Dispatch* helpers are
// exercised directly elsewhere, but the closures themselves were never
// invoked. These tests drive them through deterministic, side-effect-free
// paths (an empty install root under a temp HOME, or a forced
// resolveProfileDir failure) so no subprocess is ever spawned.

// TestPluginListCmdRunEEmptyRoot drives newPluginListCmd's RunE happy path:
// with HOME pointing at an empty temp tree the real host's install root does
// not exist, List returns no manifests, and the closure prints nothing and
// returns nil.
func TestPluginListCmdRunEEmptyRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newPluginListCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("list RunE on empty root: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("output = %q; want empty for no installed plugins", buf.String())
	}
}

// TestPluginRemoveCmdRunEMissingIsNoop drives newPluginRemoveCmd's RunE happy
// path: Host.Remove is RemoveAll under the hood, which succeeds (no-op) on a
// plugin dir that does not exist, so the closure prints the removed line.
func TestPluginRemoveCmdRunEMissingIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newPluginRemoveCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.RunE(cmd, []string{"ghost"}); err != nil {
		t.Fatalf("remove RunE: %v", err)
	}
	if !strings.Contains(buf.String(), "removed ghost") {
		t.Errorf("output = %q; want 'removed ghost'", buf.String())
	}
}

// TestPluginRemoveCmdRunEInvalidIDErrors drives newPluginRemoveCmd's RunE
// error arm: a plugin id that fails the id regex makes Host.Remove return
// ErrManifestInvalid before any filesystem touch, so the closure surfaces it.
func TestPluginRemoveCmdRunEInvalidIDErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newPluginRemoveCmd()
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.RunE(cmd, []string{"BadID"}); err == nil {
		t.Fatal("remove RunE with invalid id = nil; want manifest-invalid error")
	}
}

// TestPluginListCmdRunEReadError drives newPluginListCmd's RunE error arm:
// when the install root path exists but is a regular file (not a directory),
// ReadDir fails with a non-IsNotExist error, List propagates it, and the
// closure returns it.
func TestPluginListCmdRunEReadError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Plant a *file* where the install root directory is expected.
	root := filepath.Join(home, ".local", "share", "gum")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugins"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("plant file: %v", err)
	}
	cmd := newPluginListCmd()
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("list RunE with file-as-root = nil; want ReadDir error")
	}
}

// TestPluginRunCmdRunEUnknownPluginErrors drives newPluginRunCmd's RunE error
// arm: Host.Start fails at manifest load for a plugin that is not installed
// (before any subprocess spawn), so the closure surfaces the error.
func TestPluginRunCmdRunEUnknownPluginErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newPluginRunCmd()
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.RunE(cmd, []string{"ghost", "sometool"}); err == nil {
		t.Fatal("run RunE on unknown plugin = nil; want manifest-load error")
	}
}

// TestRegistryPluginCmdsRunEProfileDirError drives the resolveProfileDir
// failure arm shared by every registry-backed subcommand. With both
// XDG_DATA_HOME and HOME unset, resolveProfileDir cannot derive a data home
// and the closures return that error before touching the plugin host — so
// the path is deterministic and spawns nothing.
func TestRegistryPluginCmdsRunEProfileDirError(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")

	cases := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{"setup", newPluginSetupCmd, []string{"flights"}},
		{"install", newPluginInstallCmd, []string{"./somepath"}},
		{"reload", newPluginReloadCmd, []string{"flights"}},
		{"unquarantine", newPluginUnquarantineCmd, []string{"flights"}},
		{"transfer-namespace", newPluginTransferNamespaceCmd, []string{"co.example"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.cmd()
			c.SetOut(&bytes.Buffer{})
			if err := c.RunE(c, tc.args); err == nil {
				t.Fatalf("%s RunE with no data home = nil; want resolveProfileDir error", tc.name)
			}
		})
	}
}

// TestRegistryPluginCmdsRunEDispatch drives each registry-backed closure past
// resolveProfileDir into the real dispatch against an empty profile. Both
// XDG_DATA_HOME (profile root) and HOME (plugin install root) point at empty
// temp trees, so the dispatch is deterministic and spawns no subprocess: the
// missing-manifest/empty-lock paths fail (or, for unquarantine, succeed as a
// no-op) before any plugin process is started.
func TestRegistryPluginCmdsRunEDispatch(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	t.Run("setup unknown plugin errors", func(t *testing.T) {
		c := newPluginSetupCmd()
		c.SetOut(&bytes.Buffer{})
		if err := c.RunE(c, []string{"flights"}); err == nil {
			t.Fatal("setup RunE = nil; want 'plugin not configured' error")
		}
	})

	t.Run("install missing source errors", func(t *testing.T) {
		c := newPluginInstallCmd()
		c.SetOut(&bytes.Buffer{})
		if err := c.Flags().Set("yes", "true"); err != nil {
			t.Fatalf("set --yes: %v", err)
		}
		if err := c.RunE(c, []string{filepath.Join(t.TempDir(), "no-such-plugin")}); err == nil {
			t.Fatal("install RunE = nil; want stat-source error")
		}
	})

	t.Run("reload uninstalled plugin re-quarantines", func(t *testing.T) {
		c := newPluginReloadCmd()
		c.SetOut(&bytes.Buffer{})
		err := c.RunE(c, []string{"flights"})
		if err == nil {
			t.Fatal("reload RunE = nil; want passive-canary failure")
		}
		if !strings.Contains(err.Error(), "passive canary") {
			t.Errorf("reload err = %v; want 'passive canary'", err)
		}
	})

	t.Run("unquarantine absent plugin is a no-op success", func(t *testing.T) {
		c := newPluginUnquarantineCmd()
		var buf bytes.Buffer
		c.SetOut(&buf)
		if err := c.RunE(c, []string{"flights"}); err != nil {
			t.Fatalf("unquarantine RunE = %v; want nil (no-op clear)", err)
		}
		if !strings.Contains(buf.String(), "unquarantined flights") {
			t.Errorf("output = %q; want 'unquarantined flights'", buf.String())
		}
	})

	t.Run("transfer-namespace unbound prefix errors", func(t *testing.T) {
		c := newPluginTransferNamespaceCmd()
		c.SetOut(&bytes.Buffer{})
		if err := c.Flags().Set("release", "true"); err != nil {
			t.Fatalf("set --release: %v", err)
		}
		if err := c.Flags().Set("yes", "true"); err != nil {
			t.Fatalf("set --yes: %v", err)
		}
		if err := c.RunE(c, []string{"co.example"}); err == nil {
			t.Fatal("transfer-namespace RunE = nil; want prefix-not-in-lock error")
		}
	})
}

// TestPluginUnquarantineCmdRunEDispatchError drives newPluginUnquarantineCmd's
// post-dispatch error arm: resolveProfileDir succeeds, but openRegistry's
// MkdirAll fails because an ancestor of the profile dir is a regular file, so
// DispatchPluginCommandWithRegistry returns an error the closure propagates.
func TestPluginUnquarantineCmdRunEDispatchError(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	// profileDir resolves to <xdg>/gum/default; plant a file at <xdg>/gum so
	// MkdirAll of the profile dir fails with ENOTDIR.
	if err := os.WriteFile(filepath.Join(xdg, "gum"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("plant file at gum path: %v", err)
	}
	cmd := newPluginUnquarantineCmd()
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.RunE(cmd, []string{"flights"}); err == nil {
		t.Fatal("unquarantine RunE with file-blocked profile dir = nil; want mkdir error")
	}
}

// TestPluginSetupCmdRunESuccess drives newPluginSetupCmd's RunE success arm:
// an installed plugin that declares no credential_descriptors needs no
// prompting and no canary, so SetupCredentials returns nil and the closure
// prints the "configured and activated" line.
func TestPluginSetupCmdRunESuccess(t *testing.T) {
	xdg := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("HOME", home)
	// Install root the real host derives from HOME.
	installRoot := filepath.Join(home, ".local", "share", "gum", "plugins")
	writePluginSource(t, filepath.Join(installRoot, "testplug"), "testplug")

	cmd := newPluginSetupCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.RunE(cmd, []string{"testplug"}); err != nil {
		t.Fatalf("setup RunE on zero-cred plugin: %v", err)
	}
	if !strings.Contains(buf.String(), "configured") {
		t.Errorf("output = %q; want 'configured' success line", buf.String())
	}
}

// TestPluginInstallCmdRunESuccess drives newPluginInstallCmd's RunE success
// arm: installing a valid local source directory into an empty profile
// succeeds (first namespace claim) and the closure prints "installed".
func TestPluginInstallCmdRunESuccess(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	src := filepath.Join(t.TempDir(), "src")
	writePluginSource(t, src, "testplug")

	cmd := newPluginInstallCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("set --yes: %v", err)
	}
	if err := cmd.RunE(cmd, []string{src}); err != nil {
		t.Fatalf("install RunE on valid source: %v", err)
	}
	if !strings.Contains(buf.String(), "installed testplug") {
		t.Errorf("output = %q; want 'installed testplug'", buf.String())
	}
}

func TestPluginInstallCmdRunERequiresTrustAcknowledgment(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newPluginInstallCmd()
	cmd.SetOut(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{filepath.Join(t.TempDir(), "no-such-plugin")})
	if err == nil {
		t.Fatal("install without --yes returned nil; want trust acknowledgment error")
	}
	if !strings.Contains(err.Error(), "--yes is required") {
		t.Errorf("err = %v; want --yes trust acknowledgment guidance", err)
	}
}

// TestPluginTransferNamespaceCmdRunESuccess drives the transfer-namespace
// closure success arm: with a prefix already bound in the profile's
// plugins.lock, --release clears it and the closure prints the result. The
// lock is seeded through a registry pointed at the same profile dir the
// closure resolves from XDG_DATA_HOME.
func TestPluginTransferNamespaceCmdRunESuccess(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	profileDir := filepath.Join(xdg, "gum", "default")
	reg := registry.New(profileDir)
	if err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		plugins.RecordNamespaceOwner(f.Lock, "co.example", "owner-a")
		return nil
	}); err != nil {
		t.Fatalf("seed namespace owner: %v", err)
	}

	cmd := newPluginTransferNamespaceCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("release", "true"); err != nil {
		t.Fatalf("set --release: %v", err)
	}
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("set --yes: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"co.example"}); err != nil {
		t.Fatalf("transfer-namespace RunE on bound prefix: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("transfer-namespace produced no output on success")
	}
}

// TestPluginTransferNamespaceCmdRunENewOwner covers the closure's
// `--new-owner` dispatch-arg branch (distinct from the --release branch):
// re-binding a bound prefix to a different owner succeeds and prints output.
func TestPluginTransferNamespaceCmdRunENewOwner(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	profileDir := filepath.Join(xdg, "gum", "default")
	reg := registry.New(profileDir)
	if err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		plugins.RecordNamespaceOwner(f.Lock, "co.example", "owner-a")
		return nil
	}); err != nil {
		t.Fatalf("seed namespace owner: %v", err)
	}

	cmd := newPluginTransferNamespaceCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Flags().Set("new-owner", "owner-b"); err != nil {
		t.Fatalf("set --new-owner: %v", err)
	}
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatalf("set --yes: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"co.example"}); err != nil {
		t.Fatalf("transfer-namespace RunE --new-owner on bound prefix: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("transfer-namespace --new-owner produced no output on success")
	}
}
