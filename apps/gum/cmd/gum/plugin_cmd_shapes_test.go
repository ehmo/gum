package main

import (
	"strings"
	"testing"
)

// TestPluginSubcommandShapesPerCmd verifies the cobra surface of the
// per-plugin subcommands without spawning a host. Each command's Args
// validator covers the positional-arg contract the operator depends on
// (a typo at the shell will produce a clean cobra error, not an opaque
// host crash). Validators are functions, not comparable values, so the
// only way to assert their behavior is to drive them with carefully
// chosen arg counts.
func TestPluginSubcommandShapesPerCmd(t *testing.T) {
	t.Run("setup_requires_exactly_one_arg", func(t *testing.T) {
		c := newPluginSetupCmd()
		if !strings.HasPrefix(c.Use, "setup") {
			t.Errorf("Use=%q", c.Use)
		}
		if err := c.Args(c, []string{}); err == nil {
			t.Error("accepted zero args")
		}
		if err := c.Args(c, []string{"a", "b"}); err == nil {
			t.Error("accepted two args")
		}
		if err := c.Args(c, []string{"name"}); err != nil {
			t.Errorf("rejected one arg: %v", err)
		}
	})

	t.Run("transfer_namespace_requires_one_arg_and_flags", func(t *testing.T) {
		c := newPluginTransferNamespaceCmd()
		if err := c.Args(c, []string{}); err == nil {
			t.Error("accepted zero args")
		}
		if err := c.Args(c, []string{"prefix"}); err != nil {
			t.Errorf("rejected one arg: %v", err)
		}
		// Flags exist with defaults.
		if c.Flags().Lookup("new-owner") == nil {
			t.Error("--new-owner flag missing")
		}
		if c.Flags().Lookup("release") == nil {
			t.Error("--release flag missing")
		}
		if c.Flags().Lookup("yes") == nil {
			t.Error("--yes flag missing")
		}
	})

	t.Run("reload_requires_exactly_one_arg", func(t *testing.T) {
		c := newPluginReloadCmd()
		if err := c.Args(c, []string{}); err == nil {
			t.Error("accepted zero args")
		}
		if err := c.Args(c, []string{"id1", "id2"}); err == nil {
			t.Error("accepted two args")
		}
		if err := c.Args(c, []string{"id"}); err != nil {
			t.Errorf("rejected one arg: %v", err)
		}
	})

	t.Run("unquarantine_requires_exactly_one_arg", func(t *testing.T) {
		c := newPluginUnquarantineCmd()
		if err := c.Args(c, []string{}); err == nil {
			t.Error("accepted zero args")
		}
		if err := c.Args(c, []string{"id"}); err != nil {
			t.Errorf("rejected one arg: %v", err)
		}
	})

	t.Run("install_takes_one_arg_with_dev_flag", func(t *testing.T) {
		c := newPluginInstallCmd()
		// install accepts the path/spec as a single arg.
		if err := c.Args(c, []string{}); err == nil {
			t.Error("accepted zero args")
		}
		if err := c.Args(c, []string{"spec"}); err != nil {
			t.Errorf("rejected single arg: %v", err)
		}
		if c.Flags().Lookup("dev-allow-namespace-conflict") == nil {
			t.Error("--dev-allow-namespace-conflict flag missing")
		}
		if c.Flags().Lookup("yes") == nil {
			t.Error("--yes flag missing")
		}
	})
}
