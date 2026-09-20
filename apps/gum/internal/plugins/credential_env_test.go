// Spec §8.1 "Env allowlist" and §8.2 credential descriptors (gum-yq50).
//
// `gum plugin setup` stores each secret in the OS keychain under
// PluginCredentialKey(profile, plugin_id, alias). §8.1 says the spawn env
// carries "the named env vars the plugin declared in its manifest's
// needs_user_creds list", and docs/plugin-contract.md requires the host to pass
// needs_user_creds through to the subprocess. Nothing read the keychain back and
// the spawn env was built from declared_capabilities.env_allow alone, so a
// plugin that declared a credential always started without it.

package plugins_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins"
)

// memKeyring is an in-memory auth.KeyringBackend. Get returns ("", nil) for an
// absent key, matching the OS keyring contract.
type memKeyring struct{ store map[string]string }

func (m *memKeyring) Get(key string) (string, error) { return m.store[key], nil }

func (m *memKeyring) Set(key, value string) error {
	if m.store == nil {
		m.store = map[string]string{}
	}
	m.store[key] = value
	return nil
}

func (m *memKeyring) Delete(key string) error {
	delete(m.store, key)
	return nil
}

// installEnvProbePlugin installs the test binary as an MCP plugin whose echo
// tool returns its own environment. needs and descs populate the manifest's
// [requirements] block.
func installEnvProbePlugin(t *testing.T, installRoot string, needs []string, descs []map[string]any) string {
	t.Helper()
	const pluginID = "env-probe-plugin"

	pluginDir := filepath.Join(installRoot, pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
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
	if err := copyExec(src, dst); err != nil {
		t.Fatalf("copy test binary: %v", err)
	}

	manifest := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    "Env Probe Plugin",
		"version":                 "0.1.0",
		"shape":                   "mcp-plugin",
		"executable":              exeName,
		"advertised_tools": []map[string]any{
			{"name": "echo", "description": "reports env", "risk_class": "read"},
		},
		"declared_capabilities": map[string]any{
			"network":      false,
			"fs_write_dir": "",
			"env_allow":    []string{"FAKE_PLUGIN_MODE"},
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
		t.Fatalf("read test binary: %v", err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	if err := os.WriteFile(filepath.Join(pluginDir, ".executable.sha256"), []byte(digest+"\n"), 0o600); err != nil {
		t.Fatalf("write digest sidecar: %v", err)
	}

	t.Setenv("FAKE_PLUGIN_MODE", "report_env")
	return pluginID
}

// sessionDescriptors is the one-credential descriptor block used by the tests
// below: env PLUG_SESSION, alias plug_session.
func sessionDescriptors() []map[string]any {
	return []map[string]any{{
		"alias":        "plug_session",
		"env":          "PLUG_SESSION",
		"kind":         "session",
		"display_name": "Flights session cookie",
		"setup_hint":   "copy it from your browser",
	}}
}

// spawnEnv starts the probe plugin and returns its subprocess environment.
func spawnEnv(t *testing.T, h *plugins.Host, pluginID string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	plug, err := h.Start(ctx, pluginID)
	if err != nil {
		t.Fatalf("Host.Start: %v", err)
	}
	t.Cleanup(func() { _ = plug.Stop(context.Background()) })

	raw, err := plug.CallTool(ctx, "echo", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var env []string
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode env payload %s: %v", raw, err)
	}
	return env
}

// TestPluginSpawnInjectsStoredCredential is the gum-yq50 repro: a secret stored
// by `gum plugin setup` must reach the subprocess under the descriptor's env
// name.
func TestPluginSpawnInjectsStoredCredential(t *testing.T) {
	installRoot := t.TempDir()
	id := installEnvProbePlugin(t, installRoot, []string{"PLUG_SESSION"}, sessionDescriptors())

	kr := &memKeyring{store: map[string]string{
		plugins.PluginCredentialKey("default", id, "plug_session"): "s3cr3t",
	}}
	h := plugins.NewHost(plugins.HostConfig{
		InstallRoot: installRoot,
		Stderr:      testWriter{t},
		Profile:     "default",
		Keyring:     kr,
	})

	env := spawnEnv(t, h, id)
	if !slices.Contains(env, "PLUG_SESSION=s3cr3t") {
		t.Errorf("subprocess env has no PLUG_SESSION=s3cr3t; got %v", env)
	}
}

// TestPluginSpawnPassesAmbientCredential pins the §8.1 wording: a
// needs_user_creds name the user exported themselves reaches the subprocess
// even when no keychain entry exists.
func TestPluginSpawnPassesAmbientCredential(t *testing.T) {
	installRoot := t.TempDir()
	id := installEnvProbePlugin(t, installRoot, []string{"PLUG_SESSION"}, sessionDescriptors())
	t.Setenv("PLUG_SESSION", "from-shell")

	h := plugins.NewHost(plugins.HostConfig{
		InstallRoot: installRoot,
		Stderr:      testWriter{t},
	})

	env := spawnEnv(t, h, id)
	if !slices.Contains(env, "PLUG_SESSION=from-shell") {
		t.Errorf("subprocess env has no PLUG_SESSION=from-shell; got %v", env)
	}
}

// TestPluginSpawnStoredCredentialBeatsAmbient pins the precedence: the value the
// operator entered through `gum plugin setup` wins over a stale shell export.
func TestPluginSpawnStoredCredentialBeatsAmbient(t *testing.T) {
	installRoot := t.TempDir()
	id := installEnvProbePlugin(t, installRoot, []string{"PLUG_SESSION"}, sessionDescriptors())
	t.Setenv("PLUG_SESSION", "stale-shell-value")

	kr := &memKeyring{store: map[string]string{
		plugins.PluginCredentialKey("default", id, "plug_session"): "s3cr3t",
	}}
	h := plugins.NewHost(plugins.HostConfig{
		InstallRoot: installRoot,
		Stderr:      testWriter{t},
		Profile:     "default",
		Keyring:     kr,
	})

	env := spawnEnv(t, h, id)
	if !slices.Contains(env, "PLUG_SESSION=s3cr3t") {
		t.Errorf("subprocess env has no PLUG_SESSION=s3cr3t; got %v", env)
	}
	if slices.Contains(env, "PLUG_SESSION=stale-shell-value") {
		t.Error("subprocess env kept the ambient value; the stored credential must win")
	}
}

// TestPluginSpawnKeyringIsProfileScoped pins the profile in the keychain key: a
// credential stored under another profile must not leak into this spawn.
func TestPluginSpawnKeyringIsProfileScoped(t *testing.T) {
	installRoot := t.TempDir()
	id := installEnvProbePlugin(t, installRoot, []string{"PLUG_SESSION"}, sessionDescriptors())

	kr := &memKeyring{store: map[string]string{
		plugins.PluginCredentialKey("work", id, "plug_session"): "work-secret",
	}}
	h := plugins.NewHost(plugins.HostConfig{
		InstallRoot: installRoot,
		Stderr:      testWriter{t},
		Profile:     "default",
		Keyring:     kr,
	})

	env := spawnEnv(t, h, id)
	for _, e := range env {
		if e == "PLUG_SESSION=work-secret" {
			t.Error("spawn under profile 'default' picked up the 'work' profile credential")
		}
	}
}
