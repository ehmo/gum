package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// TestProfileConfigSchemaVersion is the spec §12.2 profile-config versioning
// gate: every profile config file carries `config_schema_version = 1`, a
// future version fails the load with CONFIG_SCHEMA_UNSUPPORTED, and an
// unrecognized key is a non-fatal UNKNOWN_CONFIG_KEY warning whose value
// still round-trips.
func TestProfileConfigSchemaVersion(t *testing.T) {
	t.Run("a saved config declares the current schema version", func(t *testing.T) {
		withTempConfigRootCLI(t)
		if _, err := runCLI(t, "config", "set", "output.default_format=json"); err != nil {
			t.Fatalf("config set: %v", err)
		}
		path, err := config.Path("default")
		if err != nil {
			t.Fatalf("config.Path: %v", err)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(body), "config_schema_version = 1") {
			t.Fatalf("%s does not declare config_schema_version = 1:\n%s", path, body)
		}
	})

	t.Run("a pre-normative file migrates to the current version on write", func(t *testing.T) {
		withTempConfigRootCLI(t)
		writeConfigForProfile(t, "default", "output.default_format = \"toon\"\n")

		loaded, _, err := config.Load("default")
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if loaded.SchemaVersion != 0 {
			t.Fatalf("SchemaVersion on disk = %d; want 0 for a pre-normative file", loaded.SchemaVersion)
		}
		if err := config.Save("default", loaded); err != nil {
			t.Fatalf("save: %v", err)
		}
		migrated, _, err := config.Load("default")
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if migrated.SchemaVersion != config.CurrentSchemaVersion {
			t.Fatalf("SchemaVersion after save = %d; want %d",
				migrated.SchemaVersion, config.CurrentSchemaVersion)
		}
	})

	t.Run("a future schema version fails with CONFIG_SCHEMA_UNSUPPORTED", func(t *testing.T) {
		withTempConfigRootCLI(t)
		writeConfigForProfile(t, "default", "config_schema_version = 999\noutput.default_format = \"json\"\n")

		cfg, warnings, err := config.Load("default")
		if err == nil {
			t.Fatal("load of a future schema version: want an error, got nil")
		}
		if cfg != nil || warnings != nil {
			t.Fatalf("failed load returned cfg=%v warnings=%v; want both nil", cfg, warnings)
		}
		if !strings.Contains(err.Error(), "CONFIG_SCHEMA_UNSUPPORTED") {
			t.Fatalf("error %q does not carry CONFIG_SCHEMA_UNSUPPORTED", err.Error())
		}
		if _, err := runCLI(t, "config", "get", "output.default_format"); err == nil ||
			!strings.Contains(err.Error(), "CONFIG_SCHEMA_UNSUPPORTED") {
			t.Fatalf("gum config get error = %v; want CONFIG_SCHEMA_UNSUPPORTED", err)
		}
	})

	t.Run("an unknown key warns with UNKNOWN_CONFIG_KEY and is preserved", func(t *testing.T) {
		withTempConfigRootCLI(t)
		writeConfigForProfile(t, "default",
			"config_schema_version = 1\noutput.default_format = \"toon\"\nnot.a.real.key = \"kept\"\n")

		cfg, warnings, err := config.Load("default")
		if err != nil {
			t.Fatalf("an unknown key must not fail the load: %v", err)
		}
		if got, ok := cfg.Get("not.a.real.key"); !ok || got != "kept" {
			t.Fatalf("unknown key value = (%q, %v); want (\"kept\", true)", got, ok)
		}
		var found *config.Warning
		for i := range warnings {
			if warnings[i].Key == "not.a.real.key" {
				found = &warnings[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("no warning for the unknown key; got %+v", warnings)
		}
		if found.ErrorCode != config.WarnUnknownConfigKey {
			t.Errorf("ErrorCode = %q; want %q", found.ErrorCode, config.WarnUnknownConfigKey)
		}
		if found.Event != "unknown_config_key" {
			t.Errorf("Event = %q; want unknown_config_key", found.Event)
		}
		if found.Profile != "default" {
			t.Errorf("Profile = %q; want default", found.Profile)
		}
		if found.UserMessage == "" {
			t.Error("UserMessage is empty; the operator needs the prose")
		}

		// The warning has to reach an operator, not just the caller.
		hint := doctorConfig("default").Hint
		if !strings.Contains(hint, config.WarnUnknownConfigKey) ||
			!strings.Contains(hint, "not.a.real.key") {
			t.Fatalf("doctor config hint = %q; want the code and the offending key", hint)
		}
	})
}

// writeConfigForProfile writes a config.toml for profile under the temp XDG root.
func writeConfigForProfile(t *testing.T, profile, body string) {
	t.Helper()
	path, err := config.Path(profile)
	if err != nil {
		t.Fatalf("config.Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
