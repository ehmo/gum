package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runPluginInfoCmd executes `gum plugin info` standalone and returns stdout
// plus the RunE error. The subcommand carries no --profile flag of its own, so
// the profile resolves from the environment, which is what the arms below move.
func runPluginInfoCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newPluginInfoCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// seedDefaultProfilePlugin points XDG_DATA_HOME at a temp dir and writes the
// three registry files where `gum plugin info` will look for the default
// profile.
func seedDefaultProfilePlugin(t *testing.T, status string) {
	t.Helper()
	fixture := seedPluginInfoFixture(t, status)
	dataHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", dataHome)

	profileDir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", profileDir, err)
	}
	for _, name := range []string{"plugin-catalog.json", "plugins.lock", "plugin-state.json"} {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(profileDir, name), data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// TestPluginInfoCmdPrintsTheRecord pins the whole RunE: profile resolution,
// rendering, and the write to stdout.
func TestPluginInfoCmdPrintsTheRecord(t *testing.T) {
	seedDefaultProfilePlugin(t, "active")

	out, err := runPluginInfoCmd(t, "acme")
	if err != nil {
		t.Fatalf("gum plugin info acme: %v", err)
	}
	if !strings.Contains(out, "acme") || !strings.Contains(out, "2.0.1") {
		t.Errorf("stdout missing the record:\n%s", out)
	}
}

// TestPluginInfoCmdUnknownPluginFails pins the arm that propagates the
// formatter's RESOURCE_NOT_FOUND instead of printing an empty record.
func TestPluginInfoCmdUnknownPluginFails(t *testing.T) {
	seedDefaultProfilePlugin(t, "active")

	out, err := runPluginInfoCmd(t, "nosuchplugin")
	if err == nil {
		t.Fatalf("want RESOURCE_NOT_FOUND, got output %q", out)
	}
	if !strings.Contains(err.Error(), "RESOURCE_NOT_FOUND") {
		t.Errorf("err=%v; want RESOURCE_NOT_FOUND", err)
	}
	// Cobra prints usage for a standalone subcommand; what must not appear is
	// a record for a plugin this profile does not have.
	if strings.Contains(out, "install-generation") {
		t.Errorf("stdout carries a record for an unknown plugin:\n%s", out)
	}
}

// TestPluginInfoCmdUnresolvableProfileDir pins the first RunE arm: with no
// home and no XDG_DATA_HOME there is no profile to read.
func TestPluginInfoCmdUnresolvableProfileDir(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	if _, err := runPluginInfoCmd(t, "acme"); err == nil {
		t.Fatal("want the profile-dir resolve error")
	}
}

// TestPluginInfoTextNamesTheFixingCommand pins the two remaining status hints.
// A record is worth reading because the operator can act on it, so each
// blocked state has to name the command that clears it.
func TestPluginInfoTextNamesTheFixingCommand(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"needs_configuration", "gum plugin setup acme"},
		{"installed_pending_restart", "Restart the MCP server"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			dir := seedPluginInfoFixture(t, tc.status)

			out, err := formatPluginInfo(dir, "acme", "text")
			if err != nil {
				t.Fatalf("formatPluginInfo(text): %v", err)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("text output missing %q:\n%s", tc.want, out)
			}
		})
	}
}

// TestStringFieldWithoutAnObject pins the nil guard. A record that carries no
// package or executable object must render without those rows, not panic.
func TestStringFieldWithoutAnObject(t *testing.T) {
	if got := stringField(nil, "checksum"); got != "" {
		t.Errorf("stringField(nil)=%q; want empty", got)
	}
}

// TestCredentialAliasesSkipsNonObjects pins every arm of the alias list: a
// non-object entry is skipped, a descriptor with no alias contributes nothing,
// and only the alias is printed (spec §1414 forbids raw env names).
func TestCredentialAliasesSkipsNonObjects(t *testing.T) {
	got := credentialAliases([]any{
		"not-an-object",
		map[string]any{"kind": "env", "env": "ACME_SECRET"},
		map[string]any{"alias": "acme-key", "env": "ACME_SECRET"},
		map[string]any{"alias": "acme-token"},
	})
	if got != "acme-key acme-token" {
		t.Errorf("credentialAliases=%q; want %q", got, "acme-key acme-token")
	}
	if strings.Contains(got, "ACME_SECRET") {
		t.Error("the raw env name must never reach the printed alias list")
	}
}
