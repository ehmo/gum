package main

// gum-vgip: `gum canary --plugin=<id>` spawned through plugins.Host.Start,
// which has no quarantine awareness, so a plugin the supervisor had
// permanently quarantined could still be executed. `gum plugin run` and the
// dispatch adapter were fixed for this class in gum-g7xr; the canary was a
// third call site.
//
// The re-test path a quarantined plugin needs is `gum plugin reload <id>`,
// which clears the quarantine first and then supervises the spawn, so the
// canary has no reason to keep an exception.

import (
	"bytes"
	"encoding/json"
	"testing"
)

// quarantinedProfile writes a plugin-state.json under a temp XDG data root
// marking pluginID permanently quarantined, and points HOME at a tempdir so
// no real install root resolves.
func quarantinedProfile(t *testing.T, pluginID string, permanent bool) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	root := withTempDataRootCLI(t)
	writePluginState(t, root, "default", []map[string]any{{
		"name":                 pluginID,
		"status":               "active",
		"quarantined":          true,
		"permanent_quarantine": permanent,
		"last_error_code":      "CANARY_FAILED",
	}})
}

func runCanaryCLI(t *testing.T, pluginID string) (map[string]any, error) {
	t.Helper()
	cmd := newCanaryCmd()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--plugin=" + pluginID})
	err := cmd.Execute()

	var env map[string]any
	if jerr := json.Unmarshal(stdout.Bytes(), &env); jerr != nil {
		t.Fatalf("canary stdout is not JSON: %v; got %q", jerr, stdout.String())
	}
	return env, err
}

func TestCanaryRefusesPermanentlyQuarantinedPlugin(t *testing.T) {
	quarantinedProfile(t, "acme-plug", true)

	env, err := runCanaryCLI(t, "acme-plug")
	if err == nil {
		t.Fatal("canary against a permanently quarantined plugin returned nil error")
	}
	if env["error_code"] != "VARIANT_QUARANTINED" {
		t.Errorf("error_code = %v; want VARIANT_QUARANTINED", env["error_code"])
	}
	if env["source_error_code"] != "ErrPluginQuarantined" {
		t.Errorf("source_error_code = %v; want ErrPluginQuarantined", env["source_error_code"])
	}
}

// A quarantine with no scheduled retry is the shape both the install-time
// canary and a runtime quarantine write. It must be refused too.
func TestCanaryRefusesQuarantinedPluginWithoutRetry(t *testing.T) {
	quarantinedProfile(t, "acme-plug", false)

	env, err := runCanaryCLI(t, "acme-plug")
	if err == nil {
		t.Fatal("canary against a quarantined plugin returned nil error")
	}
	if env["source_error_code"] != "ErrPluginQuarantined" {
		t.Errorf("source_error_code = %v; want ErrPluginQuarantined", env["source_error_code"])
	}
}

// A profile with no quarantine row must still reach the spawn, so the
// not-installed failure keeps its own code rather than being masked by the
// new gate.
func TestCanaryUnquarantinedPluginStillReachesSpawn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := withTempDataRootCLI(t)
	writePluginState(t, root, "default", []map[string]any{{
		"name": "acme-plug", "status": "active", "quarantined": false,
	}})

	env, err := runCanaryCLI(t, "acme-plug")
	if err == nil {
		t.Fatal("canary against a missing install returned nil error")
	}
	if env["source_error_code"] != "ErrManifestNotFound" {
		t.Errorf("source_error_code = %v; want ErrManifestNotFound", env["source_error_code"])
	}
}
