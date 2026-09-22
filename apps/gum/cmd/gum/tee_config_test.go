package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
	"github.com/ehmo/gum/internal/output/tee"
)

// Production wiring for the spec §9.0 filesystem tee (bead gum-sd58).
//
// The tee stage is what makes the §9.0 recovery contract real: it writes
// <profile>/tee/<day>/<op>/<hash>.json.gz, and only a written artifact gives
// the client _expression.full_result_path, full_result_resource and
// artifact_expires_at to poll against. dispatch.writeTeeArtifact returns early
// when TeeConfig.ProfileDir is empty, so a DispatcherConfig that omits the
// field disables the whole stage no matter what the dispatch package's own
// tests prove in isolation.

// isolateProfileHome points every XDG lookup at a fresh temp tree and returns
// the resulting <data home>/gum/default path.
func isolateProfileHome(t *testing.T) (dataHome, configHome, profileDir string) {
	t.Helper()
	root := t.TempDir()
	dataHome = filepath.Join(root, "share")
	configHome = filepath.Join(root, "config")
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	return dataHome, configHome, filepath.Join(dataHome, "gum", "default")
}

func TestDispatcherConfigWiresTheTeeStage(t *testing.T) {
	_, _, profileDir := isolateProfileHome(t)

	cfg := newDispatcherConfigForProfile("default", "default", profileDir, nil, nil)

	if cfg.Tee.ProfileDir != profileDir {
		t.Fatalf("Tee.ProfileDir = %q; want %q. An empty value makes dispatch skip every tee write, so no call ever emits full_result_path, full_result_resource or artifact_expires_at and every gum://results/{hash} read returns RESULT_ARTIFACT_EXPIRED",
			cfg.Tee.ProfileDir, profileDir)
	}
}

func TestTeeConfigForProfileDefaultsWithNoConfigFile(t *testing.T) {
	_, _, profileDir := isolateProfileHome(t)

	got := teeConfigForProfile("default", profileDir)

	if got.ProfileDir != profileDir {
		t.Errorf("ProfileDir = %q; want %q", got.ProfileDir, profileDir)
	}
	if got.Mode != "" {
		t.Errorf("Mode = %q; want \"\" so the expression profile's own tee_mode decides", got.Mode)
	}
	if got.RetentionHours != 0 {
		t.Errorf("RetentionHours = %d; want 0 so the kernel applies the spec §9.0 default", got.RetentionHours)
	}
}

func TestTeeConfigForProfileReadsGumConfig(t *testing.T) {
	_, configHome, profileDir := isolateProfileHome(t)
	writeProfileConfig(t, configHome, "output.tee_mode = \"failures\"\noutput.tee_retention_hours = \"72\"\n")

	got := teeConfigForProfile("default", profileDir)

	if got.Mode != "failures" {
		t.Errorf("Mode = %q; want failures from output.tee_mode", got.Mode)
	}
	if got.RetentionHours != 72 {
		t.Errorf("RetentionHours = %d; want 72 from output.tee_retention_hours", got.RetentionHours)
	}
}

// TestTeeConfigForProfileIgnoresBadValues pins the fail-safe reading of both
// keys. An unknown tee_mode is treated as "off" by the kernel, so accepting
// a typo would silently disable recovery for every profile that needs
// tee_mode=always; an unusable retention must fall back to the spec default
// rather than advertise an expiry the artifact does not have.
func TestTeeConfigForProfileIgnoresBadValues(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"typo mode", "output.tee_mode = \"alwys\"\noutput.tee_retention_hours = \"soon\"\n"},
		{"zero retention", "output.tee_mode = \"\"\noutput.tee_retention_hours = \"0\"\n"},
		{"negative retention", "output.tee_retention_hours = \"-5\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, configHome, profileDir := isolateProfileHome(t)
			writeProfileConfig(t, configHome, tc.body)

			got := teeConfigForProfile("default", profileDir)

			if got.Mode != "" {
				t.Errorf("Mode = %q; want \"\" for an unusable value", got.Mode)
			}
			if got.RetentionHours != 0 {
				t.Errorf("RetentionHours = %d; want 0 for an unusable value", got.RetentionHours)
			}
		})
	}
}

// TestTeeConfigForProfileSkipsUnknownProfileDir covers the degraded caller:
// a profile whose data dir could not be resolved must not enable the stage
// with an empty path, which would send tee writes to a relative "tee/" dir.
func TestTeeConfigForProfileSkipsUnknownProfileDir(t *testing.T) {
	isolateProfileHome(t)

	if got := teeConfigForProfile("default", ""); got != (dispatch.TeeConfig{}) {
		t.Errorf("teeConfigForProfile with no data dir = %+v; want the zero value", got)
	}
}

// TestTeeRetentionDefaultMatchesScanWindow pins the two halves of the expiry
// contract together: the window gum advertises and the window the
// gum://results/{hash} lookup scans come from one constant.
func TestTeeRetentionDefaultMatchesScanWindow(t *testing.T) {
	if tee.DefaultRetentionHours != 24 {
		t.Errorf("tee.DefaultRetentionHours = %d; spec §9.0 says 24", tee.DefaultRetentionHours)
	}
	if got := tee.ScanWindowDays(0); got != 2 {
		t.Errorf("ScanWindowDays(0) = %d; want 2, the default 24h window plus the UTC-day boundary", got)
	}
}

func writeProfileConfig(t *testing.T, configHome, body string) {
	t.Helper()
	dir := filepath.Join(configHome, "gum", "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
