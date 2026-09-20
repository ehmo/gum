package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// runFakePlugin serves one MCP "echo" tool over stdio. TestMain routes the test
// binary here when FAKE_PLUGIN_MODE is set, so the plugin subcommands get a
// real subprocess handshake without a second helper binary on disk.
func runFakePlugin(mode string) {
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "arms-echo", Version: "0.1.0"}, nil)
	srv.AddTool(&sdkmcp.Tool{
		Name:        "echo",
		Description: "Returns a fixed JSON payload.",
		InputSchema: map[string]any{"type": "object", "additionalProperties": true},
	}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if mode == "tool_error" {
			return nil, fmt.Errorf("upstream refused")
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: `{"ok":true}`}},
		}, nil
	})
	if err := srv.Run(context.Background(), &sdkmcp.StdioTransport{}); err != nil && err != io.EOF {
		os.Exit(2)
	}
	os.Exit(0)
}

// installArmsPlugin lays out an install dir the way Host.Install would: the
// test binary as the executable, a manifest advertising the echo tool, and the
// digest sidecar Start verifies on every spawn.
func installArmsPlugin(t *testing.T, installRoot, mode string) string {
	t.Helper()
	return installArmsPluginWithCreds(t, installRoot, mode, nil)
}

// installArmsPluginWithCreds is installArmsPlugin with a credential block.
// SetupCredentials only reaches its live canary when the manifest declares at
// least one descriptor, so the setup arms need this variant.
func installArmsPluginWithCreds(t *testing.T, installRoot, mode string, descs []map[string]any) string {
	t.Helper()
	const pluginID = "arms-echo-plugin"

	envAllow := []string{"FAKE_PLUGIN_MODE"}
	needs := make([]string, 0, len(descs))
	for _, d := range descs {
		env, _ := d["env"].(string)
		needs = append(needs, env)
		envAllow = append(envAllow, env)
	}

	pluginDir := filepath.Join(installRoot, pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pluginDir, err)
	}
	exeName := "executable"
	if runtime.GOOS == "windows" {
		exeName = "executable.exe"
	}
	dst := filepath.Join(pluginDir, exeName)
	src, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if err := copyArmsExec(src, dst); err != nil {
		t.Fatalf("copy test binary: %v", err)
	}

	manifest := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    "Arms Echo Plugin",
		"version":                 "0.1.0",
		"shape":                   "mcp-plugin",
		"executable":              exeName,
		"advertised_tools": []map[string]any{
			{"name": "echo", "description": "echoes", "risk_class": "read"},
		},
		"declared_capabilities": map[string]any{
			"network":      false,
			"fs_write_dir": "",
			"env_allow":    envAllow,
		},
		"requirements": map[string]any{
			"needs_user_creds":       needs,
			"credential_descriptors": descs,
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	raw, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read %s: %v", dst, err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	sidecar := filepath.Join(pluginDir, ".executable.sha256")
	if err := os.WriteFile(sidecar, []byte(digest+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", sidecar, err)
	}

	t.Setenv("FAKE_PLUGIN_MODE", mode)
	return pluginID
}

func copyArmsExec(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// armsPluginHome points HOME and XDG_DATA_HOME at fresh temp dirs, installs the
// fake plugin under the default install root, and returns its id. The default
// root is <home>/.local/share/gum/plugins, which is what NewHost resolves when
// HostConfig.InstallRoot is empty.
func armsPluginHome(t *testing.T, mode string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	return installArmsPlugin(t, filepath.Join(home, ".local", "share", "gum", "plugins"), mode)
}

// `gum plugin run` spawns the subprocess, calls the tool and prints the raw
// JSON result on stdout.
func TestPluginRunCmdPrintsTheToolResult(t *testing.T) {
	id := armsPluginHome(t, "echo")

	var stdout, stderr bytes.Buffer
	cmd := newPluginRunCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{id, "echo", `{"q":"x"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin run: %v (stderr %s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ok":true`) {
		t.Fatalf("stdout missing the tool payload: %q", stdout.String())
	}
}

// A tool that fails surfaces the error instead of printing a result.
func TestPluginRunCmdSurfacesAToolError(t *testing.T) {
	id := armsPluginHome(t, "tool_error")

	var stdout, stderr bytes.Buffer
	cmd := newPluginRunCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{id, "echo"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("want a tool error, got stdout %q", stdout.String())
	}
}

// `gum plugin list` prints the installed manifest on stdout.
func TestPluginListCmdPrintsAnInstalledPlugin(t *testing.T) {
	id := armsPluginHome(t, "echo")

	var stdout bytes.Buffer
	cmd := newPluginListCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin list: %v", err)
	}
	if !strings.Contains(stdout.String(), id) {
		t.Fatalf("listing missing %s:\n%s", id, stdout.String())
	}
}

// `gum plugin reload` clears quarantine, runs the passive canary against a real
// subprocess, and reports the reload on stdout.
func TestPluginReloadCmdRunsThePassiveCanary(t *testing.T) {
	id := armsPluginHome(t, "echo")

	var stdout, stderr bytes.Buffer
	cmd := newPluginReloadCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{id})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plugin reload: %v (stderr %s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "reloaded "+id) {
		t.Fatalf("stdout missing the reload line: %q", stdout.String())
	}
}

// armsKeyring is an in-memory KeyringBackend. An absent key reads back as
// ("", nil), matching the OS keyring contract.
type armsKeyring struct{ data map[string]string }

func (k *armsKeyring) Get(key string) (string, error) { return k.data[key], nil }

func (k *armsKeyring) Set(key, value string) error {
	if k.data == nil {
		k.data = map[string]string{}
	}
	k.data[key] = value
	return nil
}

func (k *armsKeyring) Delete(key string) error {
	delete(k.data, key)
	return nil
}

// With no RunCanary override, `gum plugin setup` falls back to the real host
// canary: spawn the subprocess once, then stop it. This is the path an
// operator takes; every other setup test stubs it out.
func TestPluginSetupDefaultCanarySpawnsTheSubprocess(t *testing.T) {
	root := t.TempDir()
	id := installArmsPluginWithCreds(t, root, "echo", []map[string]any{{
		"alias":        "arms_token",
		"env":          "ARMS_TOKEN",
		"kind":         "api_key",
		"display_name": "Arms Token",
		"setup_hint":   "any non-empty value",
	}})

	profileDir := t.TempDir()
	host := plugins.NewHost(plugins.HostConfig{InstallRoot: root, Profile: filepath.Base(profileDir)})

	var prompts bytes.Buffer
	out, err := DispatchPluginCommandFull(
		[]string{"setup", id},
		host,
		profileDir,
		nil,
		PluginInstallOptions{},
		PluginSetupOptions{
			InstallRoot: root,
			Keyring:     &armsKeyring{},
			In:          strings.NewReader("s3cr3t\n"),
			Out:         &prompts,
		},
	)
	if err != nil {
		t.Fatalf("plugin setup: %v (prompts %s)", err, prompts.String())
	}
	if !strings.Contains(out, "configured and activated") {
		t.Fatalf("setup result missing the activation line: %q", out)
	}
}
