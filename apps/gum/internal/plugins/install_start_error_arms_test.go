package plugins_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// armSource writes a plugin source tree whose manifest carries the given
// extra top-level JSON members, and returns the source directory. The
// executable is written as a real regular file unless withExec is false, so a
// caller can force the "declared executable is missing" install failure.
func armSource(t *testing.T, pluginID, executable, extra string, withExec bool) string {
	t.Helper()
	dir := t.TempDir()
	manifest := fmt.Sprintf(`{
  "manifest_schema_version": 1,
  "plugin_id": %q,
  "name": %q,
  "version": "0.0.1",
  "shape": "mcp-plugin",
  "namespace_owner": "arms",
  "executable": %q%s,
  "advertised_tools": [{"name":"ping","description":"Ping","risk_class":"read"}]
}`, pluginID, pluginID, executable, extra)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if withExec {
		execPath := filepath.Join(dir, executable)
		if err := os.MkdirAll(filepath.Dir(execPath), 0o755); err != nil {
			t.Fatalf("mkdir exec dir: %v", err)
		}
		if err := os.WriteFile(execPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write exec: %v", err)
		}
	}
	return dir
}

// armInstalled writes an already-installed plugin directly under installRoot,
// skipping Install so a test can plant shapes Install itself would refuse.
// The digest sidecar is written when digest is non-empty.
func armInstalled(t *testing.T, installRoot, pluginID, executable, extra, digest string) string {
	t.Helper()
	dir := filepath.Join(installRoot, pluginID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir install dir: %v", err)
	}
	manifest := fmt.Sprintf(`{
  "manifest_schema_version": 1,
  "plugin_id": %q,
  "name": %q,
  "version": "0.0.1",
  "shape": "mcp-plugin",
  "executable": %q%s,
  "advertised_tools": [{"name":"ping","description":"Ping","risk_class":"read"}]
}`, pluginID, pluginID, executable, extra)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if digest != "" {
		if err := os.WriteFile(filepath.Join(dir, ".executable.sha256"), []byte(digest+"\n"), 0o600); err != nil {
			t.Fatalf("write sidecar: %v", err)
		}
	}
	return dir
}

// sha256Hex returns the lowercase hex sha256 of path, matching the digest
// Install writes into the sidecar.
func sha256Hex(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestInstallWalkErrorArms pins the three filepath.Walk failure arms of
// Host.Install: an unreadable source subdirectory (host.go:216-218), a
// destination the host cannot create (host.go:225-228), and a source file
// that cannot be opened for copying (host.go:240-242).
func TestInstallWalkErrorArms(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("EACCES is not surfaced when running as root")
	}

	t.Run("unreadable_source_subdir", func(t *testing.T) {
		src := armSource(t, "walk-arm", "bin/plugin", "", true)
		locked := filepath.Join(src, "locked")
		if err := os.Mkdir(locked, 0o000); err != nil {
			t.Fatalf("mkdir locked: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

		host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
		_, err := host.Install(context.Background(), src)
		if err == nil {
			t.Fatal("Install err=nil; want the walk error")
		}
		if !strings.Contains(err.Error(), "plugin install:") {
			t.Errorf("err=%v; want the install wrap", err)
		}
	})

	t.Run("install_root_not_writable", func(t *testing.T) {
		src := armSource(t, "walk-arm", "bin/plugin", "", true)
		root := t.TempDir()
		if err := os.Chmod(root, 0o500); err != nil {
			t.Fatalf("chmod root: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

		host := plugins.NewHost(plugins.HostConfig{InstallRoot: root})
		if _, err := host.Install(context.Background(), src); err == nil {
			t.Fatal("Install err=nil; want the MkdirAll failure")
		}
	})

	t.Run("unreadable_source_file", func(t *testing.T) {
		src := armSource(t, "walk-arm", "bin/plugin", "", true)
		blocked := filepath.Join(src, "data.bin")
		if err := os.WriteFile(blocked, []byte("x"), 0o000); err != nil {
			t.Fatalf("write blocked file: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(blocked, 0o600) })

		host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
		_, err := host.Install(context.Background(), src)
		if err == nil {
			t.Fatal("Install err=nil; want the copy failure")
		}
		if !strings.Contains(err.Error(), "copy data.bin") {
			t.Errorf("err=%v; want the failing file named", err)
		}
	})
}

// TestInstallPostCopyArms pins the three post-copy failure arms of
// Host.Install: a declared executable that the source tree never shipped
// (host.go:258-260), an executable that copied as a directory
// (host.go:262-264), and a directory planted where the digest sidecar must
// be written (host.go:266-268).
func TestInstallPostCopyArms(t *testing.T) {
	t.Run("declared_executable_missing", func(t *testing.T) {
		src := armSource(t, "post-arm", "bin/plugin", "", false)
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
		_, err := host.Install(context.Background(), src)
		if !errors.Is(err, plugins.ErrExecutableUntrusted) {
			t.Fatalf("Install err=%v; want ErrExecutableUntrusted", err)
		}
	})

	t.Run("executable_is_a_directory", func(t *testing.T) {
		src := armSource(t, "post-arm", "bin/plugin", "", false)
		// A directory at the executable path resolves fine but cannot be
		// hashed: os.Open succeeds and the first read reports EISDIR.
		if err := os.MkdirAll(filepath.Join(src, "bin", "plugin"), 0o755); err != nil {
			t.Fatalf("mkdir exec dir: %v", err)
		}
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
		_, err := host.Install(context.Background(), src)
		if err == nil {
			t.Fatal("Install err=nil; want the hash failure")
		}
		if !strings.Contains(err.Error(), "hash executable") {
			t.Errorf("err=%v; want the hash arm", err)
		}
	})

	t.Run("sidecar_path_blocked", func(t *testing.T) {
		src := armSource(t, "post-arm", "bin/plugin", "", true)
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "post-arm", ".executable.sha256"), 0o755); err != nil {
			t.Fatalf("plant sidecar directory: %v", err)
		}
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: root})
		_, err := host.Install(context.Background(), src)
		if err == nil {
			t.Fatal("Install err=nil; want the sidecar write failure")
		}
		if !strings.Contains(err.Error(), "write digest sidecar") {
			t.Errorf("err=%v; want the sidecar arm", err)
		}
	})
}

// TestStartRefusalArms pins Start's pre-spawn refusals that no live
// subprocess is needed to reach: a path-like plugin id (host.go:364-366), an
// executable that escapes the install directory through a symlink
// (host.go:382-384), an executable that is a directory (host.go:387-389),
// and a manifest whose declared fs_write_dir escapes the work directory, so
// the sandboxed runner refuses to build the command (host.go:423-425).
func TestStartRefusalArms(t *testing.T) {
	t.Run("invalid_plugin_id", func(t *testing.T) {
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
		_, err := host.Start(context.Background(), "../escape")
		if !errors.Is(err, plugins.ErrManifestInvalid) {
			t.Fatalf("Start err=%v; want ErrManifestInvalid", err)
		}
	})

	t.Run("symlinked_executable_escapes", func(t *testing.T) {
		root := t.TempDir()
		dir := armInstalled(t, root, "startarm", "bin/plugin", "", "")
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("x"), 0o755); err != nil {
			t.Fatalf("write outside: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "bin", "plugin")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		host := plugins.NewHost(plugins.HostConfig{InstallRoot: root})
		_, err := host.Start(context.Background(), "startarm")
		if !errors.Is(err, plugins.ErrExecutableUntrusted) {
			t.Fatalf("Start err=%v; want ErrExecutableUntrusted", err)
		}
	})

	t.Run("executable_is_a_directory", func(t *testing.T) {
		root := t.TempDir()
		dir := armInstalled(t, root, "startarm", "bin/plugin", "", "")
		if err := os.MkdirAll(filepath.Join(dir, "bin", "plugin"), 0o755); err != nil {
			t.Fatalf("mkdir exec dir: %v", err)
		}
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: root})
		_, err := host.Start(context.Background(), "startarm")
		if err == nil {
			t.Fatal("Start err=nil; want the directory refusal")
		}
		if !strings.Contains(err.Error(), "is a directory") {
			t.Errorf("err=%v; want the directory arm", err)
		}
	})

	t.Run("relative_install_root", func(t *testing.T) {
		base := t.TempDir()
		t.Chdir(base)
		armInstalled(t, "relroot", "startarm", "bin/plugin", "", "")

		host := plugins.NewHost(plugins.HostConfig{InstallRoot: "relroot"})
		_, err := host.Start(context.Background(), "startarm")
		if err == nil {
			t.Fatal("Start err=nil; want the not-absolute refusal")
		}
		if !strings.Contains(err.Error(), "not absolute") {
			t.Errorf("err=%v; want the absolute-path arm", err)
		}
	})

	t.Run("sandbox_write_dir_escapes", func(t *testing.T) {
		root := t.TempDir()
		const extra = `,
  "declared_capabilities": {"fs_write_dir": "../escape"}`
		dir := armInstalled(t, root, "startarm", "bin/plugin", extra, "")
		execPath := filepath.Join(dir, "bin", "plugin")
		if err := os.MkdirAll(filepath.Dir(execPath), 0o755); err != nil {
			t.Fatalf("mkdir bin: %v", err)
		}
		if err := os.WriteFile(execPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write exec: %v", err)
		}
		// The sidecar must match, or Start refuses before it builds the command.
		digest := sha256Hex(t, execPath)
		if err := os.WriteFile(filepath.Join(dir, ".executable.sha256"), []byte(digest+"\n"), 0o600); err != nil {
			t.Fatalf("write sidecar: %v", err)
		}

		host := plugins.NewHost(plugins.HostConfig{InstallRoot: root})
		_, err := host.Start(context.Background(), "startarm")
		if err == nil {
			t.Fatal("Start err=nil; want the runner refusal")
		}
		if !strings.Contains(err.Error(), "plugin start:") {
			t.Errorf("err=%v; want the start wrap", err)
		}
	})
}

// TestInstallWithRegistryFailureArms pins the registry-aware install arms
// that the existing branch tests do not reach: an unreadable registry
// (install_registry.go:87-89), a file-copy failure propagated verbatim
// (install_registry.go:106-108), and a transaction that cannot run because
// the context is already cancelled (install_registry.go:171-173).
func TestInstallWithRegistryFailureArms(t *testing.T) {
	t.Run("registry_load_failure", func(t *testing.T) {
		src := armSource(t, "reg-arm", "bin/plugin", "", true)
		profile := t.TempDir()
		if err := os.WriteFile(registry.CatalogPath(profile), []byte("{not json"), 0o600); err != nil {
			t.Fatalf("plant catalog: %v", err)
		}
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
		_, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
			Registry: registry.New(profile),
		})
		if err == nil {
			t.Fatal("InstallWithRegistry err=nil; want the load failure")
		}
		if !strings.Contains(err.Error(), "load registry") {
			t.Errorf("err=%v; want the load-registry arm", err)
		}
	})

	t.Run("file_copy_failure_propagates", func(t *testing.T) {
		src := armSource(t, "reg-arm", "bin/plugin", "", false)
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
		_, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
			Registry: registry.New(t.TempDir()),
		})
		if !errors.Is(err, plugins.ErrExecutableUntrusted) {
			t.Fatalf("InstallWithRegistry err=%v; want the Install failure verbatim", err)
		}
	})

	t.Run("transaction_failure", func(t *testing.T) {
		src := armSource(t, "reg-arm", "bin/plugin", "", true)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
		_, err := host.InstallWithRegistry(ctx, src, plugins.InstallOptions{
			Registry: registry.New(t.TempDir()),
		})
		if err == nil {
			t.Fatal("InstallWithRegistry err=nil; want the transaction failure")
		}
		if !strings.Contains(err.Error(), "write registry") {
			t.Errorf("err=%v; want the write-registry arm", err)
		}
	})
}

// TestRemoveWithRegistryRefusalArms pins the three guards of
// RemoveWithRegistry: a missing registry (remove_registry.go:34-36), a
// path-like plugin id (remove_registry.go:37-39), and a transaction that
// cannot run because the context is already cancelled
// (remove_registry.go:61-63).
func TestRemoveWithRegistryRefusalArms(t *testing.T) {
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})

	t.Run("registry_required", func(t *testing.T) {
		err := host.RemoveWithRegistry(context.Background(), "p", plugins.RemoveOptions{})
		if err == nil || !strings.Contains(err.Error(), "registry is required") {
			t.Fatalf("err=%v; want the registry-required refusal", err)
		}
	})

	t.Run("invalid_plugin_id", func(t *testing.T) {
		err := host.RemoveWithRegistry(context.Background(), "../escape", plugins.RemoveOptions{
			Registry: registry.New(t.TempDir()),
		})
		if !errors.Is(err, plugins.ErrManifestInvalid) {
			t.Fatalf("err=%v; want ErrManifestInvalid", err)
		}
	})

	t.Run("transaction_failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := host.RemoveWithRegistry(ctx, "p", plugins.RemoveOptions{
			Registry: registry.New(t.TempDir()),
		})
		if err == nil || !strings.Contains(err.Error(), "update registry") {
			t.Fatalf("err=%v; want the update-registry arm", err)
		}
	})
}

// TestPromotePendingRestartSkipsNonPromotableRows pins the
// `!promotableAtStartup → continue` arm (pending_restart.go:52-54). The
// quarantined row shares the file with a promotable one, so the transaction
// runs and must step over it without changing its status.
func TestPromotePendingRestartSkipsNonPromotableRows(t *testing.T) {
	profile := t.TempDir()
	state := map[string]any{
		"plugin_state_schema_version": 1,
		"plugins": []any{
			map[string]any{"name": "ready", "status": "installed_pending_restart", "quarantined": false},
			map[string]any{"name": "held", "status": "installed_pending_restart", "quarantined": true},
			map[string]any{"name": "live", "status": "active", "quarantined": false},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(registry.StatePath(profile), raw, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	reg := registry.New(profile)
	promoted, err := plugins.PromotePendingRestart(context.Background(), reg, time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("PromotePendingRestart: %v", err)
	}
	if len(promoted) != 1 || promoted[0] != "ready" {
		t.Fatalf("promoted=%v; want [ready]", promoted)
	}

	files, err := reg.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, rawRow := range files.State.Plugins {
		row, ok := rawRow.(map[string]any)
		if !ok {
			t.Fatalf("row %v is not an object", rawRow)
		}
		name, _ := row["name"].(string)
		status, _ := row["status"].(string)
		if name == "held" && status != "installed_pending_restart" {
			t.Errorf("quarantined row status=%q; want it left pending", status)
		}
		if name == "live" && status != "active" {
			t.Errorf("active row status=%q; want it untouched", status)
		}
	}
}
