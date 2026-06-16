package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// TestMaybeNotifyUpdateLoadErrorReturnsSilently pins the
// `config.Load err → return` arm. If the active profile's config.toml
// cannot be read (e.g. directory planted at its path), the update-check
// heuristic MUST silently bail rather than propagating the error — this
// runs as a PostRun on every gum invocation, so an error from it would
// noisily interrupt every command. Triggered by EISDIR'ing config.toml.
func TestMaybeNotifyUpdateLoadErrorReturnsSilently(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Plant a directory where default profile's config.toml is expected.
	if err := os.MkdirAll(filepath.Join(tmp, "gum", "default", "config.toml"), 0o755); err != nil {
		t.Fatalf("plant dir blocker: %v", err)
	}

	cmd := &cobra.Command{}
	// resolveProfileFlag will hit the nil-cmd-flags branch and default
	// to "default"; we just need a *cobra.Command that won't panic when
	// PersistentFlags() is called from inside resolveProfileFlag.
	maybeNotifyUpdate(cmd)
	// Reaching here without panic is the assertion. The function MUST
	// silently swallow the err — nothing else to check.
}
