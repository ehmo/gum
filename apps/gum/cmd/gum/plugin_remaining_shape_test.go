package main

import (
	"strings"
	"testing"
)

// TestNewPluginSetupCmdShape pins the cobra wiring for `gum plugin
// setup`: Use line carries the <name> positional, ExactArgs(1)
// rejects zero/two, Short is non-empty.
func TestNewPluginSetupCmdShape(t *testing.T) {
	cmd := newPluginSetupCmd()
	if cmd.Use != "setup <name>" {
		t.Errorf("Use=%q", cmd.Use)
	}
	if cmd.Short == "" {
		t.Error("Short is empty")
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("accepted zero args")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("accepted two args")
	}
	if err := cmd.Args(cmd, []string{"only"}); err != nil {
		t.Errorf("rejected one arg: %v", err)
	}
}

// TestNewPluginReloadCmdShape mirrors the setup-cmd shape contract for
// `gum plugin reload <id>`.
func TestNewPluginReloadCmdShape(t *testing.T) {
	cmd := newPluginReloadCmd()
	if cmd.Use != "reload <id>" {
		t.Errorf("Use=%q", cmd.Use)
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("accepted zero args")
	}
}

// TestNewPluginUnquarantineCmdShape mirrors the shape contract for
// `gum plugin unquarantine <id>`.
func TestNewPluginUnquarantineCmdShape(t *testing.T) {
	cmd := newPluginUnquarantineCmd()
	if cmd.Use != "unquarantine <id>" {
		t.Errorf("Use=%q", cmd.Use)
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("accepted two args")
	}
}

// TestNewPluginInstallCmdShape pins the install flag set: the spec
// §5.1 escape-hatch flag --dev-allow-namespace-conflict and the install
// trust-acknowledgment flag --yes must exist as false-by-default bools.
func TestNewPluginInstallCmdShape(t *testing.T) {
	cmd := newPluginInstallCmd()
	if !strings.HasPrefix(cmd.Use, "install") {
		t.Errorf("Use=%q", cmd.Use)
	}
	for _, name := range []string{"dev-allow-namespace-conflict", "yes"} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Fatalf("--%s flag missing", name)
		}
		if f.DefValue != "false" {
			t.Errorf("--%s default=%q; want false", name, f.DefValue)
		}
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("accepted zero args")
	}
}

// TestNewPluginTransferNamespaceCmdShape covers the three mutator
// flags (--new-owner string, --release bool, --yes bool) and the
// ExactArgs(1) prefix gate.
func TestNewPluginTransferNamespaceCmdShape(t *testing.T) {
	cmd := newPluginTransferNamespaceCmd()
	if cmd.Use != "transfer-namespace <prefix>" {
		t.Errorf("Use=%q", cmd.Use)
	}
	for _, want := range []struct{ name, def string }{
		{"new-owner", ""},
		{"release", "false"},
		{"yes", "false"},
	} {
		f := cmd.Flags().Lookup(want.name)
		if f == nil {
			t.Errorf("flag %q missing", want.name)
			continue
		}
		if f.DefValue != want.def {
			t.Errorf("flag %q default=%q; want %q", want.name, f.DefValue, want.def)
		}
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("accepted two positional args")
	}
}

// TestNewPluginCmdAggregatesSubcommands ensures the parent `plugin`
// command wires up every subcommand the help text advertises. Drift
// here would break `gum plugin --help` discoverability.
func TestNewPluginCmdAggregatesSubcommands(t *testing.T) {
	parent := newPluginCmd()
	want := map[string]bool{
		"install":            false,
		"list":               false,
		"remove":             false,
		"run":                false,
		"setup":              false,
		"reload":             false,
		"unquarantine":       false,
		"transfer-namespace": false,
	}
	for _, sub := range parent.Commands() {
		// Use line may carry positional placeholders; first token is the verb.
		verb := strings.Fields(sub.Use)[0]
		if _, ok := want[verb]; ok {
			want[verb] = true
		}
	}
	for verb, found := range want {
		if !found {
			t.Errorf("subcommand %q not wired into newPluginCmd", verb)
		}
	}
}
