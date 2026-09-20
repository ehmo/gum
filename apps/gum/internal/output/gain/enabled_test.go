package gain_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/config"
	"github.com/ehmo/gum/internal/output/gain"
	"github.com/ehmo/gum/internal/profile"
)

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeGainConfig(t *testing.T, value string) {
	t.Helper()
	if err := config.Save("default", &config.Config{Values: map[string]string{gain.EnabledKey: value}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
}

func TestEnabledDefaultsOnWithNoConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(gain.DisabledEnv, "")

	if !gain.Enabled(profile.DefaultName) {
		t.Error("a profile with no config must keep gain accounting on")
	}
}

func TestEnabledHonorsConfigKey(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"false", false},
		{"0", false},
		{"true", true},
		{"1", true},
		// Not a bool: keep accounting on rather than silently zeroing the
		// ledger because of a typo.
		{"maybe", true},
		{"", true},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv(gain.DisabledEnv, "")
			writeGainConfig(t, tc.value)

			if got := gain.Enabled(profile.DefaultName); got != tc.want {
				t.Errorf("Enabled(gain.enabled=%q)=%v want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestEnabledEnvOverrideWins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeGainConfig(t, "true")

	t.Setenv(gain.DisabledEnv, "1")
	if gain.Enabled(profile.DefaultName) {
		t.Error("GUM_GAIN_DISABLED=1 must win over gain.enabled=true")
	}

	// Only the exact value "1" disables; any other value is not an opt-out.
	t.Setenv(gain.DisabledEnv, "yes")
	if !gain.Enabled(profile.DefaultName) {
		t.Error("GUM_GAIN_DISABLED=yes is not the documented opt-out")
	}
}

func TestEnabledIsPerProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(gain.DisabledEnv, "")
	writeGainConfig(t, "false")

	work, err := profile.Parse("work")
	if err != nil {
		t.Fatalf("parse profile: %v", err)
	}
	if !gain.Enabled(work) {
		t.Error("disabling gain on the default profile must not disable it on work")
	}
	if gain.Enabled(profile.DefaultName) {
		t.Error("default profile opt-out did not take effect")
	}
}

func TestEnabledUnreadableConfigKeepsAccounting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(gain.DisabledEnv, "")

	path, err := profile.DefaultName.ConfigPath()
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	mustWriteFile(t, path, "this is not = valid [toml\n")

	if !gain.Enabled(profile.DefaultName) {
		t.Error("an unparseable config must leave gain accounting on")
	}
}
