package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	keyringlib "github.com/zalando/go-keyring"
)

// TestCanarySurfacesAStateReadFailure pins the ReadSupervisorState arm in
// startCanaryPlugin. A plugin-state.json gum cannot parse must stop the
// canary before the spawn, not silently spawn as if no quarantine existed.
func TestCanarySurfacesAStateReadFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := withTempDataRootCLI(t)

	statePath := filepath.Join(root, "gum", "default", "plugin-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write %s: %v", statePath, err)
	}

	env, err := runCanaryCLI(t, "acme-plug")
	if err == nil {
		t.Fatal("canary over an unparseable plugin-state.json returned nil error")
	}
	if env["error_code"] != "SERVICE_DOWN" {
		t.Errorf("error_code = %v; want SERVICE_DOWN", env["error_code"])
	}
	msg, _ := env["message"].(string)
	if !bytes.Contains([]byte(msg), []byte("read plugin state")) {
		t.Errorf("message = %q; want the 'read plugin state' wrap", msg)
	}
}

// TestCanarySpawnsAndStopsAHealthyPlugin pins the success envelope. Every
// other canary test drives a failure, so the ok:true branch, the live flag
// echo and the Stop call had no coverage at all.
func TestCanarySpawnsAndStopsAHealthyPlugin(t *testing.T) {
	keyringlib.MockInit()
	id := armsPluginHome(t, "echo")

	cmd := newCanaryCmd()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--plugin=" + id, "--live"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("canary against a healthy plugin: %v (stdout %s)", err, stdout.String())
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("canary stdout is not JSON: %v; got %q", err, stdout.String())
	}
	if env["ok"] != true {
		t.Errorf("ok = %v; want true", env["ok"])
	}
	if env["plugin_id"] != id {
		t.Errorf("plugin_id = %v; want %q", env["plugin_id"], id)
	}
	if env["live"] != true {
		t.Errorf("live = %v; want true", env["live"])
	}
}
