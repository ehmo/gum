package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// fakeKeyring is an in-memory KeyringBackend used by promptAndStore /
// SetupCredentials tests. It can be configured to return a sentinel error
// on Set so we can exercise the keychain-write-failure branch without
// touching the real OS keychain.
type fakeKeyring struct {
	store map[string]string
	err   error
}

func newFakeKeyring() *fakeKeyring { return &fakeKeyring{store: map[string]string{}} }

func (f *fakeKeyring) Get(key string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.store[key], nil
}

func (f *fakeKeyring) Set(key, value string) error {
	if f.err != nil {
		return f.err
	}
	f.store[key] = value
	return nil
}

func (f *fakeKeyring) Delete(key string) error {
	delete(f.store, key)
	return nil
}

// TestSetupUserError locks the user-facing message shape: both with and
// without a suggestion the prefix must be "plugin setup:" and the
// optional suggestion is wrapped in parentheses. No raw env names or
// internal error details ever appear here (spec §1414).
func TestSetupUserError(t *testing.T) {
	t.Run("with_suggestion", func(t *testing.T) {
		err := setupUserError("missing", "run install")
		if err == nil || err.Error() != "plugin setup: missing (run install)" {
			t.Errorf("err=%v", err)
		}
	})
	t.Run("without_suggestion", func(t *testing.T) {
		err := setupUserError("missing", "")
		if err == nil || err.Error() != "plugin setup: missing" {
			t.Errorf("err=%v", err)
		}
	})
}

// TestLoadManifestByPluginID covers the thin wrapper around LoadManifest:
// a non-existent plugin directory returns an error (LoadManifest surfaces
// the io error); a present, valid manifest deserializes and returns *Manifest.
func TestLoadManifestByPluginID(t *testing.T) {
	t.Run("missing_dir_errors", func(t *testing.T) {
		installRoot := t.TempDir()
		if _, err := loadManifestByPluginID(installRoot, "ghost"); err == nil {
			t.Error("expected error for missing plugin dir")
		}
	})

	t.Run("present_manifest_loads", func(t *testing.T) {
		installRoot := t.TempDir()
		pluginID := "p"
		pluginDir := filepath.Join(installRoot, pluginID)
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			t.Fatal(err)
		}
		man := map[string]any{
			"manifest_schema_version": 1,
			"plugin_id":               pluginID,
			"name":                    "p",
			"version":                 "0.0.1",
			"shape":                   "mcp-plugin",
			"executable":              "./bin",
		}
		b, _ := json.Marshal(man)
		if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := loadManifestByPluginID(installRoot, pluginID)
		if err != nil {
			t.Fatalf("loadManifestByPluginID: %v", err)
		}
		if m == nil || m.PluginID != pluginID {
			t.Errorf("manifest=%+v", m)
		}
	})
}

// TestPromptAndStore drives the prompt → keychain pipeline. Covers:
// missing writer / reader (returns early with explicit error), empty
// input rejected as a user error using display_name (no env leak),
// happy path stores under PluginCredentialKey(profile, pluginID, alias),
// keyring failure surfaces a display_name-scoped error.
func TestPromptAndStore(t *testing.T) {
	d := CredentialDescriptor{
		Alias:       "session",
		Env:         "GUM_SESSION", // must NOT appear in any error
		Kind:        "session",
		DisplayName: "Session Cookie",
		SetupHint:   "open devtools",
	}

	t.Run("nil_writer_errors", func(t *testing.T) {
		opts := SetupOptions{In: strings.NewReader("x\n")}
		if err := promptAndStore(opts, "pid", d); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("nil_reader_errors", func(t *testing.T) {
		opts := SetupOptions{Out: &bytes.Buffer{}}
		if err := promptAndStore(opts, "pid", d); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty_input_rejected", func(t *testing.T) {
		opts := SetupOptions{
			Out: &bytes.Buffer{},
			In:  strings.NewReader("\n"),
		}
		err := promptAndStore(opts, "pid", d)
		if err == nil {
			t.Fatal("expected error")
		}
		if strings.Contains(err.Error(), "GUM_SESSION") {
			t.Errorf("env name leaked into error: %v", err)
		}
		if !strings.Contains(err.Error(), d.DisplayName) {
			t.Errorf("display_name missing from error: %v", err)
		}
	})

	t.Run("happy_path_stores", func(t *testing.T) {
		kr := newFakeKeyring()
		out := &bytes.Buffer{}
		opts := SetupOptions{
			Profile: "prof",
			Keyring: kr,
			Out:     out,
			In:      strings.NewReader("s3cret\n"),
		}
		if err := promptAndStore(opts, "pid", d); err != nil {
			t.Fatalf("promptAndStore: %v", err)
		}
		key := PluginCredentialKey("prof", "pid", d.Alias)
		if kr.store[key] != "s3cret" {
			t.Errorf("keychain[%q]=%q; want s3cret", key, kr.store[key])
		}
		if !strings.Contains(out.String(), d.DisplayName) {
			t.Errorf("prompt missing display_name: %q", out.String())
		}
		if strings.Contains(out.String(), d.Env) {
			t.Errorf("env name leaked into prompt: %q", out.String())
		}
		if !strings.Contains(out.String(), d.SetupHint) {
			t.Errorf("setup_hint missing from prompt: %q", out.String())
		}
	})

	t.Run("keyring_set_error_propagates", func(t *testing.T) {
		boom := errors.New("locked")
		kr := &fakeKeyring{store: map[string]string{}, err: boom}
		opts := SetupOptions{
			Profile: "prof",
			Keyring: kr,
			Out:     &bytes.Buffer{},
			In:      strings.NewReader("v\n"),
		}
		err := promptAndStore(opts, "pid", d)
		if err == nil || !errors.Is(err, boom) {
			t.Fatalf("err=%v; want wraps %v", err, boom)
		}
		if strings.Contains(err.Error(), d.Env) {
			t.Errorf("env name leaked into error: %v", err)
		}
	})
}

// TestSetPluginActiveAndQuarantine exercises the two registry mutations
// driven by SetupCredentials. The persisted row must carry the spec §8.6
// fields (status/activated_at OR quarantined/quarantined_at + CANARY_FAILED).
func TestSetPluginActiveAndQuarantine(t *testing.T) {
	ctx := context.Background()

	t.Run("setPluginActive_writes_status_and_clears_reason", func(t *testing.T) {
		reg := registry.New(t.TempDir())
		// Seed an existing row with a leftover reason so we can confirm it
		// gets deleted on activation.
		if err := reg.WriteTransaction(ctx, func(f *registry.Files) error {
			f.State.Plugins = append(f.State.Plugins, map[string]any{
				"name":   "p",
				"reason": "old",
			})
			return nil
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}

		now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if err := setPluginActive(ctx, reg, "p", now); err != nil {
			t.Fatalf("setPluginActive: %v", err)
		}
		files, err := reg.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(files.State.Plugins) != 1 {
			t.Fatalf("plugins len=%d; want 1", len(files.State.Plugins))
		}
		row := files.State.Plugins[0].(map[string]any)
		if row["status"] != "active" {
			t.Errorf("status=%v; want active", row["status"])
		}
		if row["activated_at"] != now.Format(time.RFC3339) {
			t.Errorf("activated_at=%v", row["activated_at"])
		}
		if _, has := row["reason"]; has {
			t.Errorf("reason should be cleared, got %v", row["reason"])
		}
	})

	t.Run("setPluginQuarantinedCANARYFailed_writes_reason", func(t *testing.T) {
		reg := registry.New(t.TempDir())
		now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		if err := setPluginQuarantinedCANARYFailed(ctx, reg, "p", now); err != nil {
			t.Fatalf("setPluginQuarantinedCANARYFailed: %v", err)
		}
		files, _ := reg.Load()
		row := files.State.Plugins[0].(map[string]any)
		if row["quarantined"] != true {
			t.Errorf("quarantined=%v; want true", row["quarantined"])
		}
		if row["last_error_code"] != "CANARY_FAILED" {
			t.Errorf("last_error_code=%v", row["last_error_code"])
		}
		if row["reason"] != "CANARY_FAILED" {
			t.Errorf("reason=%v", row["reason"])
		}
		if row["quarantined_at"] != now.Format(time.RFC3339) {
			t.Errorf("quarantined_at=%v", row["quarantined_at"])
		}
	})
}

// TestSetupCredentialsValidationGuards locks the parameter-shape gates
// in SetupCredentials that fire before any I/O. These are pure preflight
// checks; the resulting error must use the safe "plugin setup:" prefix.
func TestSetupCredentialsValidationGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("nil_registry", func(t *testing.T) {
		err := SetupCredentials(ctx, "pid", SetupOptions{Profile: "p"})
		if err == nil || !strings.Contains(err.Error(), "registry") {
			t.Errorf("err=%v; want registry-required", err)
		}
	})

	t.Run("empty_profile", func(t *testing.T) {
		err := SetupCredentials(ctx, "pid", SetupOptions{Registry: registry.New(t.TempDir())})
		if err == nil || !strings.Contains(err.Error(), "profile") {
			t.Errorf("err=%v; want profile-required", err)
		}
	})

	t.Run("missing_manifest_returns_safe_error", func(t *testing.T) {
		err := SetupCredentials(ctx, "ghost", SetupOptions{
			Registry:    registry.New(t.TempDir()),
			Profile:     "p",
			InstallRoot: t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.HasPrefix(err.Error(), "plugin setup:") {
			t.Errorf("err=%v; want plugin-setup prefix", err)
		}
		if !strings.Contains(err.Error(), "plugin install") {
			t.Errorf("err=%v; missing install hint", err)
		}
	})
}

// writeTestManifest writes a minimal plugin manifest with the given
// credential descriptors to <installRoot>/<pluginID>/manifest.json.
func writeTestManifest(t *testing.T, installRoot, pluginID string, needs []string, descs []CredentialDescriptor) {
	t.Helper()
	pluginDir := filepath.Join(installRoot, pluginID)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	man := map[string]any{
		"manifest_schema_version": 1,
		"plugin_id":               pluginID,
		"name":                    pluginID,
		"version":                 "0.0.1",
		"shape":                   "mcp-plugin",
		"executable":              "./bin",
		"requirements": map[string]any{
			"needs_user_creds":       needs,
			"credential_descriptors": descs,
		},
	}
	b, _ := json.Marshal(man)
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSetupCredentialsNoCredentialsShortCircuits proves the early return
// when the manifest declares zero credentials: no canary call, no
// registry mutation, no prompt I/O.
func TestSetupCredentialsNoCredentialsShortCircuits(t *testing.T) {
	installRoot := t.TempDir()
	writeTestManifest(t, installRoot, "p", nil, nil)

	called := false
	err := SetupCredentials(context.Background(), "p", SetupOptions{
		Registry:    registry.New(t.TempDir()),
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     newFakeKeyring(),
		RunCanary:   func(context.Context, string) error { called = true; return nil },
	})
	if err != nil {
		t.Fatalf("SetupCredentials: %v", err)
	}
	if called {
		t.Error("canary should not run when no credentials are declared")
	}
}

// TestSetupCredentialsHappyPath covers the full successful flow: prompt,
// store, canary success, registry status=active.
func TestSetupCredentialsHappyPath(t *testing.T) {
	installRoot := t.TempDir()
	descs := []CredentialDescriptor{{
		Alias: "session", Env: "GUM_SESSION", Kind: "session",
		DisplayName: "Session", SetupHint: "see docs",
	}}
	writeTestManifest(t, installRoot, "p", []string{"GUM_SESSION"}, descs)

	reg := registry.New(t.TempDir())
	kr := newFakeKeyring()
	canaryCalled := false
	err := SetupCredentials(context.Background(), "p", SetupOptions{
		Registry:    reg,
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     kr,
		In:          strings.NewReader("sekret\n"),
		Out:         &bytes.Buffer{},
		RunCanary:   func(context.Context, string) error { canaryCalled = true; return nil },
	})
	if err != nil {
		t.Fatalf("SetupCredentials: %v", err)
	}
	if !canaryCalled {
		t.Error("canary was not invoked")
	}
	key := PluginCredentialKey("prof", "p", "session")
	if kr.store[key] != "sekret" {
		t.Errorf("secret not stored: %q", kr.store[key])
	}
	files, _ := reg.Load()
	if len(files.State.Plugins) != 1 {
		t.Fatalf("plugin row not written")
	}
	if files.State.Plugins[0].(map[string]any)["status"] != "active" {
		t.Errorf("status=%v; want active", files.State.Plugins[0])
	}
}

// TestSetupCredentialsCanaryFailureQuarantines proves the §8.6 contract:
// when the canary fails, the plugin row carries quarantined=true with the
// CANARY_FAILED annotation and the user-visible error names the plugin
// (no internal canary detail leaks).
func TestSetupCredentialsCanaryFailureQuarantines(t *testing.T) {
	installRoot := t.TempDir()
	descs := []CredentialDescriptor{{
		Alias: "session", Env: "GUM_SESSION", Kind: "session",
		DisplayName: "Session",
	}}
	writeTestManifest(t, installRoot, "p", []string{"GUM_SESSION"}, descs)

	reg := registry.New(t.TempDir())
	kr := newFakeKeyring()
	err := SetupCredentials(context.Background(), "p", SetupOptions{
		Registry:    reg,
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     kr,
		In:          strings.NewReader("v\n"),
		Out:         &bytes.Buffer{},
		RunCanary:   func(context.Context, string) error { return errors.New("upstream 401") },
	})
	if err == nil {
		t.Fatal("expected canary-failure error")
	}
	if strings.Contains(err.Error(), "upstream 401") {
		t.Errorf("canary detail leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "quarantined") {
		t.Errorf("err=%v; want quarantine guidance", err)
	}

	files, _ := reg.Load()
	if len(files.State.Plugins) != 1 {
		t.Fatalf("plugin row missing")
	}
	row := files.State.Plugins[0].(map[string]any)
	if row["quarantined"] != true {
		t.Errorf("quarantined=%v; want true", row["quarantined"])
	}
	if row["last_error_code"] != "CANARY_FAILED" {
		t.Errorf("last_error_code=%v", row["last_error_code"])
	}
}

// TestSetupCredentialsManifestValidationFailure pins the §1606 guard: a
// manifest with descriptors that violate ValidateCredentialDescriptors
// (e.g. an alias not matching the regex) must return the safe summary
// without leaking the manifest's raw error.
func TestSetupCredentialsManifestValidationFailure(t *testing.T) {
	installRoot := t.TempDir()
	descs := []CredentialDescriptor{{
		Alias: "BADALIAS", // uppercase fails [a-z][a-z0-9_]{0,63}
		Env:   "GUM_X", Kind: "session", DisplayName: "X",
	}}
	writeTestManifest(t, installRoot, "p", []string{"GUM_X"}, descs)

	err := SetupCredentials(context.Background(), "p", SetupOptions{
		Registry:    registry.New(t.TempDir()),
		Profile:     "prof",
		InstallRoot: installRoot,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("err=%v; want manifest hint", err)
	}
	if strings.Contains(err.Error(), "BADALIAS") {
		t.Errorf("raw alias leaked: %v", err)
	}
}

// TestSetupCredentialsRejectsPathTraversalPluginID pins the audit fix:
// SetupCredentials validates the plugin id against pluginIDRe before it is
// joined into a filesystem path, so `../../../etc` cannot escape the install
// root (Host.Remove/Start already guarded; SetupCredentials did not).
func TestSetupCredentialsRejectsPathTraversalPluginID(t *testing.T) {
	for _, bad := range []string{"../../../etc", "..", "a/b", "ABS", "with.dot"} {
		err := SetupCredentials(context.Background(), bad, SetupOptions{})
		if !errors.Is(err, ErrManifestInvalid) {
			t.Errorf("SetupCredentials(%q): err=%v, want ErrManifestInvalid", bad, err)
		}
	}
}
