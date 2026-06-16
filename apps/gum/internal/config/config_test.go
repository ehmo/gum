package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// withTempConfigRoot redirects XDG_CONFIG_HOME to a t.TempDir() so each test
// has an isolated config tree. Returns the redirected root.
func withTempConfigRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	return root
}

// writeConfigFile writes raw content to the profile's config.toml path within
// the current XDG_CONFIG_HOME, creating parent directories as needed.
func writeConfigFile(t *testing.T, profile, content string) {
	t.Helper()
	p, err := config.Path(profile)
	if err != nil {
		t.Fatalf("config.Path(%q): %v", profile, err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}

// TestConfigPathHonorsXDG verifies that Path() respects $XDG_CONFIG_HOME and
// falls back to $HOME/.config when XDG is unset.
func TestConfigPathHonorsXDG(t *testing.T) {
	tmp := t.TempDir()

	// With XDG_CONFIG_HOME set.
	t.Setenv("XDG_CONFIG_HOME", tmp)
	got, err := config.Path("default")
	if err != nil {
		t.Fatalf("Path(\"default\") with XDG set: %v", err)
	}
	want := filepath.Join(tmp, "gum", "default", "config.toml")
	if got != want {
		t.Errorf("Path with XDG: got %q, want %q", got, want)
	}

	// Without XDG_CONFIG_HOME — fall back to $HOME/.config.
	t.Setenv("XDG_CONFIG_HOME", "")
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	got2, err := config.Path("default")
	if err != nil {
		t.Fatalf("Path(\"default\") without XDG: %v", err)
	}
	want2 := filepath.Join(homeDir, ".config", "gum", "default", "config.toml")
	if got2 != want2 {
		t.Errorf("Path without XDG: got %q, want %q", got2, want2)
	}
}

// TestLoadMissingFileReturnsEmpty verifies that a missing config file is not
// an error — it returns an empty Config with SchemaVersion 0.
func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	withTempConfigRoot(t)

	c, warnings, err := config.Load("default")
	if err != nil {
		t.Fatalf("Load on missing file: unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("Load on missing file: got nil Config, want empty Config")
	}
	if c.SchemaVersion != 0 {
		t.Errorf("SchemaVersion: got %d, want 0", c.SchemaVersion)
	}
	if len(c.Values) != 0 {
		t.Errorf("Values: got %v, want empty map", c.Values)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings: got %d, want 0", len(warnings))
	}
}

// TestSaveRoundTripsAndMigrates verifies that Save migrates SchemaVersion 0 to
// 1 and that the round-tripped value is returned by Load/Get.
func TestSaveRoundTripsAndMigrates(t *testing.T) {
	withTempConfigRoot(t)

	c := &config.Config{SchemaVersion: 0}
	c.Set("output.default_format", "json")

	if err := config.Save("default", c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	c2, warnings, err := config.Load("default")
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if c2.SchemaVersion != 1 {
		t.Errorf("SchemaVersion after Save: got %d, want 1 (migration expected)", c2.SchemaVersion)
	}
	val, ok := c2.Get("output.default_format")
	if !ok {
		t.Error("Get(\"output.default_format\"): key not found after round-trip")
	}
	if val != "json" {
		t.Errorf("Get(\"output.default_format\"): got %q, want %q", val, "json")
	}
	if len(warnings) != 0 {
		t.Errorf("warnings after clean round-trip: got %d, want 0", len(warnings))
	}
}

// TestLoadRejectsFutureSchemaVersion verifies that a config file with a future
// schema version returns CONFIG_SCHEMA_UNSUPPORTED.
func TestLoadRejectsFutureSchemaVersion(t *testing.T) {
	withTempConfigRoot(t)
	writeConfigFile(t, "default", `config_schema_version = 999
output.default_format = "json"
`)

	c, warnings, err := config.Load("default")
	if err == nil {
		t.Fatal("Load with future schema version: expected error, got nil")
	}
	if c != nil {
		t.Errorf("Load with future schema version: got non-nil Config, want nil")
	}
	if warnings != nil {
		t.Errorf("Load with future schema version: got non-nil warnings, want nil")
	}

	var errSchema *config.ErrSchemaUnsupported
	if !errors.As(err, &errSchema) {
		t.Fatalf("error type: expected *config.ErrSchemaUnsupported, got %T: %v", err, err)
	}
	if errSchema.Profile != "default" {
		t.Errorf("ErrSchemaUnsupported.Profile: got %q, want %q", errSchema.Profile, "default")
	}
	if errSchema.Version != 999 {
		t.Errorf("ErrSchemaUnsupported.Version: got %d, want 999", errSchema.Version)
	}

	wantPrefix := "CONFIG_SCHEMA_UNSUPPORTED: profile 'default' config_schema_version=999"
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Errorf("error message: got %q, want prefix %q", err.Error(), wantPrefix)
	}
}

// TestLoadPreservesUnknownKeysWithWarning verifies that unknown key prefixes
// are preserved in Values and produce a structured Warning (non-fatal).
func TestLoadPreservesUnknownKeysWithWarning(t *testing.T) {
	withTempConfigRoot(t)
	writeConfigFile(t, "default", `config_schema_version = 1
output.default_format = "toon"
totally.unknown.key = "preserved"
`)

	c, warnings, err := config.Load("default")
	if err != nil {
		t.Fatalf("Load with unknown key: unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("Load with unknown key: got nil Config")
	}
	if c.SchemaVersion != 1 {
		t.Errorf("SchemaVersion: got %d, want 1", c.SchemaVersion)
	}

	val, ok := c.Get("output.default_format")
	if !ok || val != "toon" {
		t.Errorf("Get(\"output.default_format\"): got (%q, %v), want (\"toon\", true)", val, ok)
	}

	val2, ok2 := c.Get("totally.unknown.key")
	if !ok2 || val2 != "preserved" {
		t.Errorf("Get(\"totally.unknown.key\"): got (%q, %v), want (\"preserved\", true)", val2, ok2)
	}

	if len(warnings) < 1 {
		t.Fatalf("warnings: got %d, want >= 1", len(warnings))
	}
	w := warnings[0]
	if w.Event != "unknown_config_key" {
		t.Errorf("warnings[0].Event: got %q, want %q", w.Event, "unknown_config_key")
	}
	if w.Key != "totally.unknown.key" {
		t.Errorf("warnings[0].Key: got %q, want %q", w.Key, "totally.unknown.key")
	}
	if w.Profile != "default" {
		t.Errorf("warnings[0].Profile: got %q, want %q", w.Profile, "default")
	}
	if w.UserMessage == "" {
		t.Error("warnings[0].UserMessage: got empty string, want non-empty")
	}
}

// TestSavePersistsKnownAndUnknownKeysVerbatim verifies that all keys —
// including those with unknown prefixes — survive a Save/Load round-trip.
func TestSavePersistsKnownAndUnknownKeysVerbatim(t *testing.T) {
	withTempConfigRoot(t)

	c := &config.Config{SchemaVersion: 0}
	c.Set("output.default_format", "json")
	c.Set("audit.unbounded", "true")
	c.Set("totally.unknown.key", "preserved")

	if err := config.Save("default", c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	c2, _, err := config.Load("default")
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if c2.SchemaVersion != 1 {
		t.Errorf("SchemaVersion: got %d, want 1", c2.SchemaVersion)
	}

	cases := []struct{ key, want string }{
		{"output.default_format", "json"},
		{"audit.unbounded", "true"},
		{"totally.unknown.key", "preserved"},
	}
	for _, tc := range cases {
		got, ok := c2.Get(tc.key)
		if !ok {
			t.Errorf("Get(%q): key not found after round-trip", tc.key)
			continue
		}
		if got != tc.want {
			t.Errorf("Get(%q): got %q, want %q", tc.key, got, tc.want)
		}
	}
}

// TestSaveFileMode600 verifies that Save writes the config file with mode 0600.
func TestSaveFileMode600(t *testing.T) {
	withTempConfigRoot(t)

	c := &config.Config{SchemaVersion: 0}
	c.Set("output.default_format", "json")

	if err := config.Save("default", c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	p, err := config.Path("default")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode: got %04o, want 0600", mode)
	}
}

// TestConfigUnset covers the three branches of (*Config).Unset:
//   - Nil receiver / nil Values → false (no panic).
//   - Missing key on populated config → false.
//   - Present key → true and the key is gone from Values.
func TestConfigUnset(t *testing.T) {
	t.Run("nil_receiver_returns_false", func(t *testing.T) {
		var c *config.Config
		if c.Unset("anything") {
			t.Errorf("nil receiver should return false")
		}
	})

	t.Run("nil_values_returns_false", func(t *testing.T) {
		c := &config.Config{}
		if c.Unset("anything") {
			t.Errorf("nil Values should return false")
		}
	})

	t.Run("missing_key_returns_false", func(t *testing.T) {
		c := &config.Config{}
		c.Set("kept", "v")
		if c.Unset("missing") {
			t.Errorf("missing key should return false")
		}
		if _, ok := c.Get("kept"); !ok {
			t.Errorf("Unset on missing key removed the wrong entry")
		}
	})

	t.Run("present_key_returns_true_and_removes", func(t *testing.T) {
		c := &config.Config{}
		c.Set("doomed", "v")
		if !c.Unset("doomed") {
			t.Errorf("present key should return true")
		}
		if _, ok := c.Get("doomed"); ok {
			t.Errorf("Unset did not remove key")
		}
	})
}

// TestPathRejectsTraversalProfile pins the audit fix: a profile name containing
// path separators or ".." is rejected, so a crafted --profile can't escape the
// gum config directory.
func TestPathRejectsTraversalProfile(t *testing.T) {
	for _, p := range []string{"../evil", "..", "a/b", "x/../y", `a\b`} {
		if _, err := config.Path(p); err == nil {
			t.Errorf("config.Path(%q) = nil error; want rejection (path traversal)", p)
		}
	}
}
