package plugins

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestSetupCredentialsReadsEveryDescriptorFromOneStream proves a manifest
// with two credential descriptors consumes two lines from one input stream.
// Each prompt used to build its own bufio.Scanner over opts.In, and the first
// scanner buffers everything it can read, so the second prompt saw EOF and
// `gum plugin setup` refused a plugin that needs two secrets.
func TestSetupCredentialsReadsEveryDescriptorFromOneStream(t *testing.T) {
	installRoot := t.TempDir()
	descs := []CredentialDescriptor{
		{Alias: "session", Env: "PLUG_SESSION", Kind: "session", DisplayName: "Session"},
		{Alias: "apikey", Env: "PLUG_API_KEY", Kind: "api_key", DisplayName: "API key"},
	}
	writeTestManifest(t, installRoot, "p", []string{"PLUG_SESSION", "PLUG_API_KEY"}, descs)

	kr := newFakeKeyring()
	err := SetupCredentials(context.Background(), "p", SetupOptions{
		Registry:    registry.New(t.TempDir()),
		Profile:     "prof",
		InstallRoot: installRoot,
		Keyring:     kr,
		In:          strings.NewReader("first-secret\nsecond-secret\n"),
		Out:         &bytes.Buffer{},
		RunCanary:   func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("SetupCredentials: %v", err)
	}
	for alias, want := range map[string]string{"session": "first-secret", "apikey": "second-secret"} {
		key := PluginCredentialKey("prof", "p", alias)
		if got := kr.store[key]; got != want {
			t.Errorf("keychain[%s]=%q; want %q", alias, got, want)
		}
	}
}

// TestSetupCredentialsDefaultsInstallRootToHome covers the fallback that runs
// when the caller names no install root: the path is derived from the home
// directory, so a profile with no manifest there reports "plugin not
// configured" rather than reading somebody else's plugin.
func TestSetupCredentialsDefaultsInstallRootToHome(t *testing.T) {
	for name, home := range map[string]string{"home_set": t.TempDir(), "home_unset": ""} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HOME", home)
			err := SetupCredentials(context.Background(), "p", SetupOptions{
				Registry: registry.New(t.TempDir()),
				Profile:  "prof",
				Keyring:  newFakeKeyring(),
				In:       strings.NewReader("secret\n"),
				Out:      &bytes.Buffer{},
			})
			if err == nil {
				t.Fatal("SetupCredentials err=nil; want plugin-not-configured")
			}
			if !strings.Contains(err.Error(), "plugin not configured") {
				t.Errorf("err=%v; want plugin not configured", err)
			}
		})
	}
}

// TestPromptAndStoreSurfacesReadError covers the arm that separates a broken
// stream from an empty one: a read failure must not be reported as "no input
// provided", which would send the user looking at their typing.
func TestPromptAndStoreSurfacesReadError(t *testing.T) {
	boom := errors.New("stdin went away")
	opts := SetupOptions{Profile: "prof", Keyring: newFakeKeyring(), Out: &bytes.Buffer{}}
	err := promptAndStore(opts, newSecretReader(errReader{err: boom}), "p", CredentialDescriptor{
		Alias: "session", Env: "PLUG_SESSION", Kind: "session", DisplayName: "Session",
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v; want wrapped %v", err, boom)
	}
	if !strings.Contains(err.Error(), "reading input") {
		t.Errorf("err=%v; want it to name the read step", err)
	}
}
