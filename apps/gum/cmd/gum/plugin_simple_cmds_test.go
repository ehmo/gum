package main

import (
	"strings"
	"testing"
)

// TestNewPluginListCmdShape locks the cobra wiring without spawning any
// subprocess: the command must exist, accept no positional args, and have
// a non-empty Short. We only verify the static shape — the actual
// plugin-host dispatch is exercised elsewhere via DispatchPluginCommand.
func TestNewPluginListCmdShape(t *testing.T) {
	cmd := newPluginListCmd()
	if cmd.Use != "list" {
		t.Errorf("Use = %q, want list", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short is empty")
	}
	// cobra.NoArgs rejects any positional argument.
	if err := cmd.Args(cmd, []string{"extra"}); err == nil {
		t.Errorf("Args accepted positional arg; want rejection")
	}
}

// TestNewPluginRemoveCmdArgs locks the ExactArgs(1) gate: the command
// must reject zero and >1 positional args at validation time.
func TestNewPluginRemoveCmdArgs(t *testing.T) {
	cmd := newPluginRemoveCmd()
	if cmd.Use != "remove <id>" {
		t.Errorf("Use = %q", cmd.Use)
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("Args accepted zero args; want rejection")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("Args accepted two args; want rejection")
	}
	if err := cmd.Args(cmd, []string{"only-one"}); err != nil {
		t.Errorf("Args rejected single arg: %v", err)
	}
}

// TestNewPluginRunCmdArgs locks the RangeArgs(2,3) gate covering the
// optional [args-json] trailing argument.
func TestNewPluginRunCmdArgs(t *testing.T) {
	cmd := newPluginRunCmd()
	if !strings.Contains(cmd.Use, "run") {
		t.Errorf("Use = %q, missing run", cmd.Use)
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("Args accepted zero args; want rejection")
	}
	if err := cmd.Args(cmd, []string{"id"}); err == nil {
		t.Error("Args accepted one arg; want rejection")
	}
	if err := cmd.Args(cmd, []string{"id", "tool"}); err != nil {
		t.Errorf("Args rejected 2 args: %v", err)
	}
	if err := cmd.Args(cmd, []string{"id", "tool", "{}"}); err != nil {
		t.Errorf("Args rejected 3 args: %v", err)
	}
	if err := cmd.Args(cmd, []string{"id", "tool", "{}", "extra"}); err == nil {
		t.Error("Args accepted 4 args; want rejection")
	}
}
