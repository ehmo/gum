package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// TestGetNilReceiverReturnsEmptyFalse pins the
// `c == nil || c.Values == nil → return "", false` defensive arm.
// Callers may hold a (*Config)(nil) returned from a failed Load OR
// a Config whose Values map was never initialized (zero-value Config);
// Get MUST cleanly surface ("", false) rather than NPE on the map
// access — this is a common pattern in default-then-override config
// helpers.
func TestGetNilReceiverReturnsEmptyFalse(t *testing.T) {
	var nilC *config.Config
	if v, ok := nilC.Get("any"); v != "" || ok {
		t.Errorf("(nil).Get = (%q, %v); want (\"\", false)", v, ok)
	}
	zero := &config.Config{} // Values is nil
	if v, ok := zero.Get("any"); v != "" || ok {
		t.Errorf("zero.Get = (%q, %v); want (\"\", false)", v, ok)
	}
}

// TestPathEmptyProfileDefaultsToDefault pins the
// `profile == "" → profile = "default"` arm. config.Path is called
// from every Load/Save and from `gum config path`; an empty profile
// MUST normalize to "default" so a forgotten --profile flag doesn't
// land config.toml in a malformed "<base>/gum//config.toml" hole.
func TestPathEmptyProfileDefaultsToDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got, err := config.Path("")
	if err != nil {
		t.Fatalf("Path(\"\"): %v", err)
	}
	want := filepath.Join(tmp, "gum", "default", "config.toml")
	if got != want {
		t.Errorf("Path(\"\")=%q; want %q", got, want)
	}
}

// TestLoadReadFileEISDIRWrapsAsConfigReadError pins the
// `err != nil && !os.IsNotExist(err) → "config: read ...:"` arm.
// Planting a directory at config.toml's path makes ReadFile return
// EISDIR, which is neither ENOENT (the silent default-empty arm)
// nor a parse error — Load MUST surface a "config: read" wrap so
// operators see the exact failure type.
func TestLoadReadFileEISDIRWrapsAsConfigReadError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Plant a directory where config.toml is expected to live.
	cfgDir := filepath.Join(tmp, "gum", "p")
	if err := os.MkdirAll(filepath.Join(cfgDir, "config.toml"), 0o755); err != nil {
		t.Fatalf("plant dir blocker: %v", err)
	}

	_, _, err := config.Load("p")
	if err == nil {
		t.Fatal("want EISDIR-shaped err; got nil")
	}
	if !strings.Contains(err.Error(), "config: read") {
		t.Errorf("err=%v; want 'config: read' wrap", err)
	}
}

// TestSaveTempFileFailureSurfacesAsSaveError pins the arm where the temp file
// cannot be created. Save delegates to fsatomic, which creates its temp file in
// the destination directory, so a read-only config dir is what makes that step
// fail. The wrap must name the config path and the failing fsatomic step, or an
// operator cannot tell a permissions problem from a full disk.
func TestSaveTempFileFailureSurfacesAsSaveError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: mode bits do not deny writes")
	}
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// MkdirAll on an existing directory succeeds regardless of its mode, so
	// Save gets past the mkdir and fails at temp-file creation.
	cfgDir := filepath.Join(tmp, "gum", "p")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("mkdir cfgDir: %v", err)
	}
	if err := os.Chmod(cfgDir, 0o500); err != nil {
		t.Fatalf("chmod cfgDir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfgDir, 0o700) })

	c := &config.Config{Values: map[string]string{"k": "v"}}
	err := config.Save("p", c)
	if err == nil {
		t.Fatal("Save(read-only dir) = nil; want a temp-file failure")
	}
	if !strings.Contains(err.Error(), "config: save") || !strings.Contains(err.Error(), "tempfile") {
		t.Errorf("err=%v; want a 'config: save' wrap naming 'tempfile'", err)
	}
}
