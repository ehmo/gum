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

// TestSaveWriteTmpFailWrapsAsWriteTmpError pins the
// `WriteFile tmp err → "config: write tmp"` arm. Planting a directory
// at the <path>.tmp location makes WriteFile fail with EISDIR; Save
// MUST surface the "write tmp" wrap rather than fall through to
// Chmod or Rename which would then mis-attribute the failure.
func TestSaveWriteTmpFailWrapsAsWriteTmpError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Pre-create both the parent AND a directory at <path>.tmp so
	// MkdirAll succeeds but WriteFile fails.
	cfgDir := filepath.Join(tmp, "gum", "p")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("mkdir cfgDir: %v", err)
	}
	tmpBlocker := filepath.Join(cfgDir, "config.toml.tmp")
	if err := os.Mkdir(tmpBlocker, 0o755); err != nil {
		t.Fatalf("plant tmp dir: %v", err)
	}

	c := &config.Config{Values: map[string]string{"k": "v"}}
	err := config.Save("p", c)
	if err == nil {
		t.Fatal("want write-tmp err; got nil")
	}
	if !strings.Contains(err.Error(), "write tmp") {
		t.Errorf("err=%v; want 'write tmp' wrap", err)
	}
}
