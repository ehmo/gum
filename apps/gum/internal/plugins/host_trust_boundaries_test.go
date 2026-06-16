package plugins_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

func TestLoadManifestRejectsExecutableEscape(t *testing.T) {
	for _, executable := range []string{filepath.Join(t.TempDir(), "plug"), "../plug"} {
		t.Run(executable, func(t *testing.T) {
			dir := writeMinimalPluginSource(t, "trust-test", executable)
			_, err := plugins.LoadManifest(dir)
			if !errors.Is(err, plugins.ErrManifestInvalid) {
				t.Fatalf("LoadManifest executable=%q err=%v; want ErrManifestInvalid", executable, err)
			}
		})
	}
}

func TestInstallRejectsSymlinkSource(t *testing.T) {
	src := writeMinimalPluginSource(t, "trust-test", "bin/plugin")
	if err := os.Remove(filepath.Join(src, "bin", "plugin")); err != nil {
		t.Fatalf("remove real executable: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside-plugin")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write outside executable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "bin", "plugin")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	host := plugins.NewHost(plugins.HostConfig{InstallRoot: t.TempDir()})
	_, err := host.Install(context.Background(), src)
	if !errors.Is(err, plugins.ErrExecutableUntrusted) {
		t.Fatalf("Install symlink executable err=%v; want ErrExecutableUntrusted", err)
	}
}

func writeMinimalPluginSource(t *testing.T, pluginID, executable string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := fmt.Sprintf(`{
  "manifest_schema_version": 1,
  "plugin_id": %q,
  "name": %q,
  "version": "0.0.1",
  "shape": "mcp-plugin",
  "executable": %q,
  "advertised_tools": [{"name":"ping","description":"Ping","risk_class":"read"}]
}`, pluginID, pluginID, executable)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	execPath := filepath.Join(dir, filepath.Clean(executable))
	if !filepath.IsAbs(executable) && !startsWithParent(filepath.Clean(executable)) {
		if err := os.MkdirAll(filepath.Dir(execPath), 0o755); err != nil {
			t.Fatalf("mkdir exec dir: %v", err)
		}
		if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write exec: %v", err)
		}
	}
	return dir
}

func startsWithParent(path string) bool {
	return path == ".." || len(path) > 3 && path[:3] == "../"
}

// TestLoadManifestRejectsPathLikeToolName is the audit hardening: an advertised
// tool name becomes the op_id suffix plug.<id>.<name>, so a path-like /
// whitespace / control-char name from an untrusted manifest must be rejected
// before it pollutes catalog keys, audit op_ids, and MCP tool names.
func TestLoadManifestRejectsPathLikeToolName(t *testing.T) {
	for _, badName := range []string{"../escape", "a/b", "has space", "UpperCase", ".leadingdot", "tab\tname"} {
		t.Run(badName, func(t *testing.T) {
			dir := t.TempDir()
			manifest := `{
  "manifest_schema_version": 1,
  "plugin_id": "trust-test",
  "name": "trust-test",
  "version": "0.0.1",
  "shape": "mcp-plugin",
  "executable": "bin/plugin",
  "advertised_tools": [{"name":` + strconvQuote(badName) + `,"description":"x","risk_class":"read"}]
}`
			if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			_, err := plugins.LoadManifest(dir)
			if !errors.Is(err, plugins.ErrManifestInvalid) {
				t.Fatalf("LoadManifest tool name=%q err=%v; want ErrManifestInvalid", badName, err)
			}
		})
	}
}

func strconvQuote(s string) string { return fmt.Sprintf("%q", s) }
