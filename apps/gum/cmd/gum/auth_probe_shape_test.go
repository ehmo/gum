package main

import (
	"strings"
	"testing"

	keyringlib "github.com/zalando/go-keyring"
)

// TestNewAuthProbeCmdShape pins the cobra surface: the probe subcommand
// must exist with a --scopes flag defaulting to gmail.readonly so the
// `gum auth probe` smoke test stays one-keystroke for operators.
func TestNewAuthProbeCmdShape(t *testing.T) {
	cmd := newAuthProbeCmd()
	if cmd.Use != "probe" {
		t.Errorf("Use=%q; want probe", cmd.Use)
	}
	f := cmd.Flags().Lookup("scopes")
	if f == nil {
		t.Fatal("--scopes flag missing")
	}
	if !strings.Contains(f.DefValue, "gmail.readonly") {
		t.Errorf("--scopes default=%q; want it to contain gmail.readonly", f.DefValue)
	}
}

// TestNewAuthLoginCmdNoClientPointsToSetup pins the redesigned ergonomics:
// `gum auth login` no longer demands an explicit --scope. With no OAuth client
// registered yet it must point the operator at `gum auth use-oauth-client`
// rather than complaining about scopes — the user shouldn't have to guess
// scope strings.
func TestNewAuthLoginCmdNoClientPointsToSetup(t *testing.T) {
	keyringlib.MockInit()
	t.Cleanup(keyringlib.MockInit)

	cmd := newAuthLoginCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no OAuth client is configured")
	}
	if !strings.Contains(err.Error(), "use-oauth-client") {
		t.Errorf("err=%q; want pointer to `gum auth use-oauth-client`", err)
	}
	if strings.Contains(err.Error(), "--scope is required") {
		t.Errorf("err=%q; should NOT demand an explicit --scope anymore", err)
	}
}
