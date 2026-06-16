package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProfileIsDevConfigLoadErrorReturnsFalse pins the
// `config.Load err != nil → return false` arm. Spec §5.1 mandates that
// production profiles default to the strict gate, so a malformed
// config.toml MUST NOT accidentally enable the dev-allow-namespace-
// conflict flag — profileIsDev MUST treat any Load failure as "not dev."
func TestProfileIsDevConfigLoadErrorReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "gum", "brokenprofile")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A line without `=` is a parse error in internal/config.
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("notakeyvalueline\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if profileIsDev("brokenprofile") {
		t.Error("profileIsDev with malformed config.toml = true; want false (Load err must default to strict gate)")
	}
}
