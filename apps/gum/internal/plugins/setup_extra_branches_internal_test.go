package plugins

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestSetupCredentialsPromptAndStoreErrorWrapsWithAlias pins the
// `promptAndStore err → return "credential %q" wrap` arm
// (setup.go:101-104). Reached when opts.In is empty so scanner.Scan
// returns false with no error, surfacing "no input provided" — which
// SetupCredentials MUST wrap with the credential alias (NOT the raw
// env var name).
func TestSetupCredentialsPromptAndStoreErrorWrapsWithAlias(t *testing.T) {
	installRoot := t.TempDir()
	descs := []CredentialDescriptor{{
		Alias:       "session",
		Env:         "GUM_SECRET_ENV", // raw env var that MUST NOT appear in errors
		Kind:        "session",
		DisplayName: "Session",
		SetupHint:   "see docs",
	}}
	writeTestManifest(t, installRoot, "p", []string{"GUM_SECRET_ENV"}, descs)

	err := SetupCredentials(context.Background(), "p", SetupOptions{
		Registry:    registry.New(t.TempDir()),
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     newFakeKeyring(),
		In:          strings.NewReader(""), // empty stdin
		Out:         &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("SetupCredentials(empty stdin) err=nil; want promptAndStore err wrap")
	}
	if !strings.Contains(err.Error(), "session") {
		t.Errorf("err=%v; want credential alias 'session' in wrap", err)
	}
	if strings.Contains(err.Error(), "GUM_SECRET_ENV") {
		t.Errorf("err leaks raw env var name: %v", err)
	}
}

// TestSetupCredentialsCanaryNilUsesDefaultSuccess pins the
// `canaryFn == nil → assign no-op` arm (setup.go:109-110). Reached
// when opts.RunCanary is nil — SetupCredentials substitutes a
// success-returning closure so the happy path still marks the
// plugin active without forcing every caller to pass a canary.
func TestSetupCredentialsCanaryNilUsesDefaultSuccess(t *testing.T) {
	installRoot := t.TempDir()
	descs := []CredentialDescriptor{{
		Alias:       "session",
		Env:         "GUM_SECRET_ENV",
		Kind:        "session",
		DisplayName: "Session",
	}}
	writeTestManifest(t, installRoot, "p", []string{"GUM_SECRET_ENV"}, descs)

	reg := registry.New(t.TempDir())
	err := SetupCredentials(context.Background(), "p", SetupOptions{
		Registry:    reg,
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     newFakeKeyring(),
		In:          strings.NewReader("the-secret\n"),
		Out:         &bytes.Buffer{},
		RunCanary:   nil, // exercises the default-fallback arm
	})
	if err != nil {
		t.Fatalf("SetupCredentials with nil canary: %v", err)
	}
	files, _ := reg.Load()
	if len(files.State.Plugins) != 1 {
		t.Fatalf("plugin row not written")
	}
	row, _ := files.State.Plugins[0].(map[string]any)
	if status, _ := row["status"].(string); status != "active" {
		t.Errorf("status=%q; want active (nil canary should default to success)", status)
	}
}
