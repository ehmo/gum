package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileIsDev exercises the profile.is_dev resolver. Missing profile →
// false (production-strict default); is_dev=true → true; is_dev=1 → true (the
// docs-permitted boolean synonym).
func TestProfileIsDev(t *testing.T) {
	// Point XDG_CONFIG_HOME at a tempdir so profiles never bleed across tests.
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	t.Run("missing_profile_returns_false", func(t *testing.T) {
		if profileIsDev("doesnotexist") {
			t.Errorf("profileIsDev(missing) = true, want false (production default)")
		}
	})

	t.Run("explicit_true", func(t *testing.T) {
		setupDevProfile(t, tmp, "devprofile", "true")
		if !profileIsDev("devprofile") {
			t.Errorf("profileIsDev(is_dev=true) = false, want true")
		}
	})

	t.Run("explicit_1", func(t *testing.T) {
		setupDevProfile(t, tmp, "oneprofile", "1")
		if !profileIsDev("oneprofile") {
			t.Errorf("profileIsDev(is_dev=1) = false, want true")
		}
	})

	t.Run("explicit_false", func(t *testing.T) {
		setupDevProfile(t, tmp, "prodprofile", "false")
		if profileIsDev("prodprofile") {
			t.Errorf("profileIsDev(is_dev=false) = true, want false")
		}
	})
}

// setupDevProfile writes a config.toml under <xdg>/gum/<profile>/config.toml
// with profile.is_dev set to value. Mirrors what `gum config set` does.
// The on-disk format is flat key=value pairs (internal/config), so the dotted
// key is written literally as "profile.is_dev = ..."
func setupDevProfile(t *testing.T, xdg, profile, value string) {
	t.Helper()
	dir := filepath.Join(xdg, "gum", profile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "profile.is_dev = \"" + value + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// TestResolveProfileDir covers the XDG_DATA_HOME-honouring profile-dir
// resolver. The override path wins; absent that, the helper falls back to
// $HOME/.local/share/gum/<profile>.
func TestResolveProfileDir(t *testing.T) {
	t.Run("xdg_data_home_wins", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmp)
		got, err := resolveProfileDir("alpha")
		if err != nil {
			t.Fatalf("resolveProfileDir: %v", err)
		}
		want := filepath.Join(tmp, "gum", "alpha")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("empty_profile_defaults_to_default", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmp)
		got, err := resolveProfileDir("")
		if err != nil {
			t.Fatalf("resolveProfileDir: %v", err)
		}
		if !strings.HasSuffix(got, filepath.Join("gum", "default")) {
			t.Errorf("empty profile = %q, want gum/default suffix", got)
		}
	})

	t.Run("home_fallback_when_no_xdg", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		got, err := resolveProfileDir("beta")
		if err != nil {
			t.Skipf("home dir unavailable in this env: %v", err)
		}
		if !strings.HasSuffix(got, filepath.Join(".local", "share", "gum", "beta")) {
			t.Errorf("got %q, want .local/share/gum/beta suffix", got)
		}
	})
}

// TestDefaultPluginsHost only verifies the factory returns a non-nil host
// implementing the interface. The host itself is exercised by plugins/* tests.
func TestDefaultPluginsHost(t *testing.T) {
	h := defaultPluginsHost()
	if h == nil {
		t.Fatal("defaultPluginsHost returned nil")
	}
}
