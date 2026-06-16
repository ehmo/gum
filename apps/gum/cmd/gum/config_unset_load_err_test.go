package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigUnsetLoadErrorSurfacesAsError pins the
// `config.Load err → return err` arm of newConfigUnsetCmd. Planting a
// directory at config.toml's path forces ReadFile to surface EISDIR;
// the unset command MUST propagate that error rather than falling
// through to call Unset on a nil *Config (which would NPE on a
// shadowed nil-receiver Get path).
func TestConfigUnsetLoadErrorSurfacesAsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Plant a directory where config.toml is expected to live for the
	// default profile.
	cfgDir := filepath.Join(tmp, "gum", "default")
	if err := os.MkdirAll(filepath.Join(cfgDir, "config.toml"), 0o755); err != nil {
		t.Fatalf("plant dir blocker: %v", err)
	}

	_, err := runCLI(t, "config", "unset", "any.key")
	if err == nil {
		t.Fatal("want load-err propagation; got nil")
	}
	// Underlying err message contains 'config: read' wrap from config.Load.
	if !strings.Contains(err.Error(), "config: read") {
		t.Errorf("err=%v; want 'config: read' substr (load-err surface)", err)
	}
}
