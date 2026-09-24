package plugins_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/pluginenv"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// writeCommandPlugin writes a plugin source tree whose manifest carries the
// given `command` selector. The executable is always `bin/mcp`.
func writeCommandPlugin(t *testing.T, pluginID string, command []string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "bin", "mcp"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	man := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    pluginID,
		"version":                 "1.0.0",
		"namespace_owner":         "acme",
		"shape":                   "mcp-plugin",
		"executable":              "bin/mcp",
		"advertised_tools": []map[string]any{{
			"name":       "ping",
			"risk_class": "read",
		}},
	}
	if command != nil {
		man["command"] = command
	}
	b, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

func installCommandPlugin(t *testing.T, pluginID string, command []string, dev bool) (string, *registry.Registry, error) {
	t.Helper()
	src := writeCommandPlugin(t, pluginID, command)
	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: installRoot})
	_, err := host.InstallWithRegistry(context.Background(), src, plugins.InstallOptions{
		Registry:  reg,
		Namespace: plugins.NamespaceOptions{ProfileIsDev: dev},
	})
	return installRoot, reg, err
}

func lockRow(t *testing.T, reg *registry.Registry, pluginID string) map[string]any {
	t.Helper()
	files, err := reg.Load()
	if err != nil {
		t.Fatalf("registry load: %v", err)
	}
	for _, raw := range files.Lock.Plugins {
		row, ok := raw.(map[string]any)
		if ok && row["name"] == pluginID {
			return row
		}
	}
	t.Fatalf("no lock row for %q in %#v", pluginID, files.Lock.Plugins)
	return nil
}

// TestPluginCommandNormalization pins docs/test-matrix.md and spec
// §8.7 "Install-time command normalization": the author's `command` is a
// selector resolved once at install, the profile's plugins.lock records
// executable path, digest, install root and normalized argv, and every
// resolution path that would leave the verified install root is refused
// with PLUGIN_EXECUTABLE_UNTRUSTED.
func TestPluginCommandNormalization(t *testing.T) {
	t.Run("no command yields the bare executable", func(t *testing.T) {
		installRoot, reg, err := installCommandPlugin(t, "plain", nil, false)
		if err != nil {
			t.Fatalf("install: %v", err)
		}
		want := []string{filepath.Join(installRoot, "plain", "bin", "mcp")}
		assertArgv(t, lockRow(t, reg, "plain"), want)
	})

	t.Run("matching selector keeps residual args", func(t *testing.T) {
		installRoot, reg, err := installCommandPlugin(t, "resid", []string{"bin/mcp", "mcp", "--stdio"}, false)
		if err != nil {
			t.Fatalf("install: %v", err)
		}
		want := []string{filepath.Join(installRoot, "resid", "bin", "mcp"), "mcp", "--stdio"}
		assertArgv(t, lockRow(t, reg, "resid"), want)
	})

	t.Run("dot-slash selector matches", func(t *testing.T) {
		installRoot, reg, err := installCommandPlugin(t, "dotslash", []string{"./bin/mcp", "serve"}, false)
		if err != nil {
			t.Fatalf("install: %v", err)
		}
		want := []string{filepath.Join(installRoot, "dotslash", "bin", "mcp"), "serve"}
		assertArgv(t, lockRow(t, reg, "dotslash"), want)
	})

	// The refusal table. Each selector is a resolution path spec §8.7
	// forbids outside dev profiles.
	refusals := []struct {
		name    string
		command []string
	}{
		{"PATH-only uvx", []string{"uvx", "fli", "mcp"}},
		{"PATH-only bare name", []string{"mcp"}},
		{"shell interpreter", []string{"/bin/sh", "-c", "./bin/mcp"}},
		{"absolute path", []string{"/usr/local/bin/mcp"}},
		{"traversal", []string{"../outside/bin/mcp"}},
		{"wrapper beside the executable", []string{"bin/wrapper.sh"}},
		{"empty selector", []string{"", "mcp"}},
	}
	for _, tc := range refusals {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			installRoot, reg, err := installCommandPlugin(t, "refuse", tc.command, false)
			if !errors.Is(err, plugins.ErrExecutableUntrusted) {
				t.Fatalf("install err = %v; want PLUGIN_EXECUTABLE_UNTRUSTED", err)
			}
			files, loadErr := reg.Load()
			if loadErr != nil {
				t.Fatalf("registry load: %v", loadErr)
			}
			if len(files.Lock.Plugins) != 0 {
				t.Errorf("refused install wrote %d lock rows; want 0", len(files.Lock.Plugins))
			}
			// The refusal precedes the copy, so nothing reaches disk.
			if _, statErr := os.Stat(filepath.Join(installRoot, "refuse")); !os.IsNotExist(statErr) {
				t.Errorf("refused install left an install dir (stat err = %v)", statErr)
			}
		})
	}

	t.Run("dev profile accepts an unresolvable selector", func(t *testing.T) {
		installRoot, reg, err := installCommandPlugin(t, "devp", []string{"uvx", "fli", "mcp"}, true)
		if err != nil {
			t.Fatalf("dev install: %v", err)
		}
		// The declared executable still wins; uvx is never spawned.
		want := []string{filepath.Join(installRoot, "devp", "bin", "mcp"), "fli", "mcp"}
		assertArgv(t, lockRow(t, reg, "devp"), want)
	})

	t.Run("lock records the full binding", func(t *testing.T) {
		installRoot, reg, err := installCommandPlugin(t, "bind", []string{"bin/mcp", "serve"}, false)
		if err != nil {
			t.Fatalf("install: %v", err)
		}
		row := lockRow(t, reg, "bind")
		installDir := filepath.Join(installRoot, "bind")
		if got := row["install_root"]; got != installDir {
			t.Errorf("install_root = %v; want %s", got, installDir)
		}
		execPath := filepath.Join(installDir, "bin", "mcp")
		if got := row["executable_path"]; got != execPath {
			t.Errorf("executable_path = %v; want %s", got, execPath)
		}
		if s, _ := row["executable_sha256"].(string); len(s) != 64 {
			t.Errorf("executable_sha256 = %v; want a 64-char digest", row["executable_sha256"])
		}
	})
}

// assertArgv compares a lock row's argv_normalized against want. The row is
// read back through the registry, so the values arrive as []any.
func assertArgv(t *testing.T, row map[string]any, want []string) {
	t.Helper()
	raw, ok := row["argv_normalized"].([]any)
	if !ok {
		t.Fatalf("argv_normalized missing or wrong type in %#v", row)
	}
	got := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("argv_normalized element %#v is not a string", v)
		}
		got = append(got, s)
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("argv_normalized = %#v; want %#v", got, want)
	}
}

// TestNormalizedArgvReachesSubprocess proves the residual tokens are not
// decoration: the runner passes them to the spawned process. Without this
// the lock would record an argv gum never launches.
func TestNormalizedArgvReachesSubprocess(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "mcp")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s|' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	cmd, err := pluginenv.NewRunner(pluginenv.RunnerConfig{
		Executable: script,
		Args:       []string{"mcp", "--stdio"},
		Stdout:     &stdout,
	}).Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := stdout.String(); got != "mcp|--stdio|" {
		t.Errorf("subprocess argv = %q; want %q", got, "mcp|--stdio|")
	}
}
