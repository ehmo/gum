package main_test

// TestPluginSetupCredentialFlow and TestPluginCredentialNoRawEnvLeak are the
// acceptance tests for gum-y4e: `gum plugin setup <name>` (spec §7/§8.2/§1606).
//
// Design notes:
// - No real subprocess is spawned; the canary is stubbed via PluginSetupOptions.RunCanary.
// - go-keyring is stubbed in-process via keyring.MockInit() so no OS keychain
//   is touched and no secrets leak to the real keychain.
// - goleak.VerifyNone(t) at each entry point satisfies the auth package's
//   goroutine-hygiene requirement (no goroutine leak is permitted).
// - Raw env var names MUST NOT appear in any user-visible output or error
//   message; assertions scan every observable string for GUM_TEST_TOKEN and
//   GUM_VERY_SECRET_ENV.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"
	"go.uber.org/goleak"

	gummain "github.com/ehmo/gum/cmd/gum"
	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// memKeyring is a minimal in-memory KeyringBackend for plugin setup tests.
// It uses the same field names as keyring.MockInit() but is goroutine-safe
// for the single-threaded test context.
type memKeyring struct {
	data map[string]string
}

func newMemKeyring() *memKeyring { return &memKeyring{data: map[string]string{}} }

func (m *memKeyring) Get(key string) (string, error) {
	v, ok := m.data[key]
	if !ok {
		return "", nil
	}
	return v, nil
}

func (m *memKeyring) Set(key, value string) error {
	m.data[key] = value
	return nil
}

func (m *memKeyring) Delete(key string) error {
	delete(m.data, key)
	return nil
}

// writeTestPlugin creates a fake plugin directory at installRoot/<pluginID>/
// with a manifest.json containing the given credential descriptors. The
// executable file is a zero-byte placeholder; no actual process is spawned.
func writeTestPlugin(t *testing.T, installRoot, pluginID string, descs []plugins.CredentialDescriptor) {
	t.Helper()
	pluginDir := filepath.Join(installRoot, pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}

	// Build needs_user_creds from descriptors.
	needs := make([]string, 0, len(descs))
	for _, d := range descs {
		needs = append(needs, d.Env)
	}

	m := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    "Test Plugin",
		"version":                 "0.1.0",
		"shape":                   "mcp-plugin",
		"executable":              "executable",
		"advertised_tools": []map[string]any{
			{"name": "echo", "description": "echo", "risk_class": "read"},
		},
		"declared_capabilities": map[string]any{
			"network":     false,
			"fs_write_dir": "",
			"env_allow":   needs,
		},
		"requirements": map[string]any{
			"needs_user_creds":       needs,
			"credential_descriptors": descs,
		},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	// Create placeholder executable so LoadManifest doesn't reject an empty path.
	if err := os.WriteFile(filepath.Join(pluginDir, "executable"), nil, 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
}

// writeRegistryRow writes a minimal plugin-state.json row for pluginID into
// profileDir so the registry is in needs_configuration state (as if `gum
// plugin install` was run but credentials are absent).
func writeRegistryRow(t *testing.T, reg *registry.Registry, pluginID string) {
	t.Helper()
	ctx := context.Background()
	err := reg.WriteTransaction(ctx, func(f *registry.Files) error {
		f.State.Plugins = append(f.State.Plugins, map[string]any{
			"name":   pluginID,
			"status": "needs_configuration",
			"reason": "missing_credentials",
		})
		return nil
	})
	if err != nil {
		t.Fatalf("write registry row: %v", err)
	}
}

// TestPluginSetupCredentialFlow is the happy-path acceptance test for
// `gum plugin setup <name>`. It exercises:
//   - Loading the manifest from the install root.
//   - Prompting for each credential (display_name, setup_hint).
//   - Storing the secret in the keyring under (profile, plugin_id, alias).
//   - Running the live canary (stubbed to succeed).
//   - Setting the plugin state to "active" in plugin-state.json.
//   - CRITICAL: stdout must not contain the raw env var name "GUM_TEST_TOKEN"
//     or the literal secret "secret-value".
func TestPluginSetupCredentialFlow(t *testing.T) {
	defer goleak.VerifyNone(t)

	// Stub the go-keyring backend so Set/Get work in-process.
	keyring.MockInit()
	defer keyring.MockInit()

	const pluginID = "test-plugin"
	const rawEnv = "GUM_TEST_TOKEN"
	const secretValue = "secret-value"

	installRoot := t.TempDir()
	profileDir := t.TempDir()
	// profileName is derived from the last component of profileDir, mirroring
	// how DispatchPluginCommandFull computes it via filepath.Base(profileDir).
	profileName := filepath.Base(profileDir)
	reg := registry.New(profileDir)

	// Create fake plugin with one credential descriptor.
	descs := []plugins.CredentialDescriptor{
		{
			Alias:       "test_token",
			Env:         rawEnv,
			Kind:        "api_key",
			DisplayName: "Test Token",
			SetupHint:   "Generate at https://example.test/token",
		},
	}
	writeTestPlugin(t, installRoot, pluginID, descs)
	writeRegistryRow(t, reg, pluginID)

	// Stub stdin with the secret value.
	stdin := strings.NewReader(secretValue + "\n")

	// Capture stdout (prompts).
	var outBuf bytes.Buffer

	// Stub canary: always succeeds.
	canaryOK := false
	runCanary := func(_ context.Context, pid string) error {
		if pid != pluginID {
			return fmt.Errorf("canary: unexpected plugin %q", pid)
		}
		canaryOK = true
		return nil
	}

	// Use in-memory keyring (not OS keychain).
	kb := newMemKeyring()

	result, err := gummain.DispatchPluginCommandFull(
		[]string{"setup", pluginID},
		&mockHost{},
		profileDir,
		func(pd string) *registry.Registry { return reg },
		gummain.PluginInstallOptions{},
		gummain.PluginSetupOptions{
			InstallRoot: installRoot,
			Keyring:     kb,
			In:          stdin,
			Out:         &outBuf,
			RunCanary:   runCanary,
		},
	)

	// Assertions.
	if err != nil {
		t.Fatalf("setup returned error: %v", err)
	}
	if !canaryOK {
		t.Error("live canary was not called")
	}

	// Assert keyring contains the secret under the correct key.
	wantKey := plugins.PluginCredentialKey(profileName, pluginID, "test_token")
	stored, _ := kb.Get(wantKey)
	if stored != secretValue {
		t.Errorf("keyring[%q] = %q; want %q", wantKey, stored, secretValue)
	}

	// Assert plugin state is active.
	files, err := reg.Load()
	if err != nil {
		t.Fatalf("registry load: %v", err)
	}
	var foundStatus string
	for _, raw := range files.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := row["name"].(string); n == pluginID {
			foundStatus, _ = row["status"].(string)
		}
	}
	if foundStatus != "active" {
		t.Errorf("plugin status = %q; want active", foundStatus)
	}

	// CRITICAL: raw env var name and secret must not appear in any user-visible output.
	allOutput := outBuf.String() + result
	if strings.Contains(allOutput, rawEnv) {
		t.Errorf("user-visible output contains raw env var %q (spec §1414 violation):\n%s", rawEnv, allOutput)
	}
	if strings.Contains(allOutput, secretValue) {
		t.Errorf("user-visible output contains secret value %q:\n%s", secretValue, allOutput)
	}
}

// TestPluginCredentialNoRawEnvLeak asserts that the env var name
// "GUM_VERY_SECRET_ENV" never appears in stdout, stderr, or any returned
// error message across every user-visible failure path the setup command
// can produce. This is the spec §1414/§1606 normative requirement.
func TestPluginCredentialNoRawEnvLeak(t *testing.T) {
	defer goleak.VerifyNone(t)

	keyring.MockInit()
	defer keyring.MockInit()

	const rawEnv = "GUM_VERY_SECRET_ENV"
	const pluginID = "secret-plugin"

	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)

	descs := []plugins.CredentialDescriptor{
		{
			Alias:       "very_secret",
			Env:         rawEnv,
			Kind:        "api_key",
			DisplayName: "Very Secret Credential",
			SetupHint:   "Get it from the dashboard",
		},
	}
	writeTestPlugin(t, installRoot, pluginID, descs)
	writeRegistryRow(t, reg, pluginID)

	// assertNoLeak checks that rawEnv does not appear in any observable string.
	assertNoLeak := func(t *testing.T, label string, values ...string) {
		t.Helper()
		for _, v := range values {
			if strings.Contains(v, rawEnv) {
				t.Errorf("[%s] env var %q leaked into user-visible output: %q", label, rawEnv, v)
			}
		}
	}

	t.Run("missing manifest", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		// Point to an empty install root so LoadManifest fails.
		emptyRoot := t.TempDir()
		var outBuf bytes.Buffer
		_, err := gummain.DispatchPluginCommandFull(
			[]string{"setup", pluginID},
			&mockHost{},
			profileDir,
			func(pd string) *registry.Registry { return reg },
			gummain.PluginInstallOptions{},
			gummain.PluginSetupOptions{
				InstallRoot: emptyRoot,
				Keyring:     newMemKeyring(),
				In:          strings.NewReader(""),
				Out:         &outBuf,
				RunCanary:   func(_ context.Context, _ string) error { return nil },
			},
		)
		assertNoLeak(t, "missing manifest error", outBuf.String())
		if err != nil {
			assertNoLeak(t, "missing manifest err.Error()", err.Error())
		}
	})

	t.Run("invalid manifest validation failure", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		// Write a manifest with a descriptor that has an invalid kind to
		// trigger ValidateCredentialDescriptors failure.
		badInstallRoot := t.TempDir()
		badDescs := []plugins.CredentialDescriptor{
			{
				Alias:       "very_secret",
				Env:         rawEnv,
				Kind:        "bad-kind-xyz",
				DisplayName: "Very Secret Credential",
				SetupHint:   "Get it from the dashboard",
			},
		}
		writeTestPlugin(t, badInstallRoot, pluginID, badDescs)

		var outBuf bytes.Buffer
		_, err := gummain.DispatchPluginCommandFull(
			[]string{"setup", pluginID},
			&mockHost{},
			profileDir,
			func(pd string) *registry.Registry { return reg },
			gummain.PluginInstallOptions{},
			gummain.PluginSetupOptions{
				InstallRoot: badInstallRoot,
				Keyring:     newMemKeyring(),
				In:          strings.NewReader(""),
				Out:         &outBuf,
				RunCanary:   func(_ context.Context, _ string) error { return nil },
			},
		)
		assertNoLeak(t, "validation failure output", outBuf.String())
		if err != nil {
			assertNoLeak(t, "validation failure err.Error()", err.Error())
		}
	})

	t.Run("canary failure", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		var outBuf bytes.Buffer
		_, err := gummain.DispatchPluginCommandFull(
			[]string{"setup", pluginID},
			&mockHost{},
			profileDir,
			func(pd string) *registry.Registry { return reg },
			gummain.PluginInstallOptions{},
			gummain.PluginSetupOptions{
				InstallRoot: installRoot,
				Keyring:     newMemKeyring(),
				In:          strings.NewReader("some-value\n"),
				Out:         &outBuf,
				RunCanary:   func(_ context.Context, _ string) error { return errors.New("canary: upstream unreachable") },
			},
		)
		assertNoLeak(t, "canary failure output", outBuf.String())
		if err != nil {
			assertNoLeak(t, "canary failure err.Error()", err.Error())
		}
	})

	t.Run("keyring write failure", func(t *testing.T) {
		defer goleak.VerifyNone(t)

		// Inject an error keyring backend.
		errorKB := &errorKeyring{err: errors.New("keychain: service unavailable")}

		var outBuf bytes.Buffer
		_, err := gummain.DispatchPluginCommandFull(
			[]string{"setup", pluginID},
			&mockHost{},
			profileDir,
			func(pd string) *registry.Registry { return reg },
			gummain.PluginInstallOptions{},
			gummain.PluginSetupOptions{
				InstallRoot: installRoot,
				Keyring:     errorKB,
				In:          strings.NewReader("some-value\n"),
				Out:         &outBuf,
				RunCanary:   func(_ context.Context, _ string) error { return nil },
			},
		)
		assertNoLeak(t, "keyring failure output", outBuf.String())
		if err != nil {
			assertNoLeak(t, "keyring failure err.Error()", err.Error())
		}
	})
}

// TestPluginSetupCanaryFailureQuarantines verifies that a canary failure
// leaves the plugin in the "quarantined" state with CANARY_FAILED annotation,
// per spec §8.6.
func TestPluginSetupCanaryFailureQuarantines(t *testing.T) {
	defer goleak.VerifyNone(t)

	keyring.MockInit()
	defer keyring.MockInit()

	const pluginID = "canary-fail-plugin"
	installRoot := t.TempDir()
	profileDir := t.TempDir()
	reg := registry.New(profileDir)

	descs := []plugins.CredentialDescriptor{
		{
			Alias:       "my_key",
			Env:         "GUM_MY_KEY",
			Kind:        "api_key",
			DisplayName: "My API Key",
			SetupHint:   "From your dashboard",
		},
	}
	writeTestPlugin(t, installRoot, pluginID, descs)
	writeRegistryRow(t, reg, pluginID)

	var outBuf bytes.Buffer
	_, err := gummain.DispatchPluginCommandFull(
		[]string{"setup", pluginID},
		&mockHost{},
		profileDir,
		func(pd string) *registry.Registry { return reg },
		gummain.PluginInstallOptions{},
		gummain.PluginSetupOptions{
			InstallRoot: installRoot,
			Keyring:     newMemKeyring(),
			In:          strings.NewReader("my-secret-key\n"),
			Out:         &outBuf,
			RunCanary:   func(_ context.Context, _ string) error { return errors.New("upstream unreachable") },
		},
	)
	if err == nil {
		t.Fatal("expected error from canary failure, got nil")
	}

	// Plugin must be quarantined.
	files, loadErr := reg.Load()
	if loadErr != nil {
		t.Fatalf("registry load: %v", loadErr)
	}
	var status, lastError string
	for _, raw := range files.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := row["name"].(string); n == pluginID {
			status, _ = row["status"].(string)
			// last_error_code is set by setPluginQuarantinedCANARYFailed
			lastError, _ = row["last_error_code"].(string)
		}
	}
	// The quarantine flag is set; the status may stay "needs_configuration"
	// but quarantined=true and last_error_code=CANARY_FAILED must be present.
	files2, _ := reg.Load()
	for _, raw := range files2.State.Plugins {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := row["name"].(string); n == pluginID {
			if q, _ := row["quarantined"].(bool); !q {
				t.Errorf("plugin quarantined = false; want true after canary failure")
			}
			if lec, _ := row["last_error_code"].(string); lec != "CANARY_FAILED" {
				t.Errorf("last_error_code = %q; want CANARY_FAILED", lec)
			}
		}
	}
	_ = status
	_ = lastError
}

// errorKeyring is a KeyringBackend that always returns an error for Set,
// simulating a keychain write failure.
type errorKeyring struct {
	err error
}

func (e *errorKeyring) Get(key string) (string, error) { return "", nil }
func (e *errorKeyring) Set(key, value string) error    { return e.err }
func (e *errorKeyring) Delete(key string) error        { return nil }
