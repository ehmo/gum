package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestCanaryRejectsMissingPlugin — gum-xepy acceptance line 2. The CLI must
// surface a SERVICE_DOWN-shaped error envelope when the operator points it at
// a plugin that isn't installed under the configured InstallRoot. The shape
// matches spec §8 error_code projection: an unknown/missing plugin maps to
// SERVICE_DOWN (the "plugin not installed" failure shape). The envelope must
// expose plugin_id so the operator can act on it.
func TestCanaryRejectsMissingPlugin(t *testing.T) {
	// Isolate the install root: plugins.NewHost defaults to
	// <UserHomeDir>/.local/share/gum/plugins, so pointing HOME at a tempdir
	// guarantees the plugin id under test cannot accidentally resolve to a
	// pre-existing install on the dev machine.
	t.Setenv("HOME", t.TempDir())

	cmd := newCanaryCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--plugin=google-flights-missing"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("canary against missing plugin returned nil error; expected SERVICE_DOWN")
	}

	// Envelope is emitted on stdout so it's machine-parseable.
	var env map[string]any
	if jerr := json.Unmarshal(stdout.Bytes(), &env); jerr != nil {
		t.Fatalf("canary stdout is not JSON: %v; got %q", jerr, stdout.String())
	}
	if env["error_code"] != "SERVICE_DOWN" {
		t.Errorf("error_code = %v; want SERVICE_DOWN", env["error_code"])
	}
	if env["plugin_id"] != "google-flights-missing" {
		t.Errorf("plugin_id = %v; want google-flights-missing", env["plugin_id"])
	}
}

// TestCanaryRequiresPluginFlag — guards the simplest mis-invocation: the
// operator forgot --plugin. cobra must surface a usage error, not a nil-deref
// panic on the empty plugin id.
func TestCanaryRequiresPluginFlag(t *testing.T) {
	cmd := newCanaryCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("canary without --plugin returned nil error; expected required-flag error")
	}
	if !strings.Contains(err.Error(), "plugin") {
		t.Errorf("err = %v; want message mentioning the missing --plugin flag", err)
	}
}
