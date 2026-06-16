package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/config"
)

// TestSaveRejectsNilConfig pins the nil-config guard. Without it,
// Save would NPE on c.SchemaVersion below — a sharp edge for callers
// that defer Save(Load(...)) without a nil-check.
func TestSaveRejectsNilConfig(t *testing.T) {
	err := config.Save("default", nil)
	if err == nil {
		t.Fatalf("Save(nil) returned nil; want error")
	}
	if !strings.Contains(err.Error(), "nil config") {
		t.Errorf("err=%q; want nil-config message", err)
	}
}

// TestSaveMkdirAllErrorSurfacesPath drives the MkdirAll failure branch
// by parking a regular file where MkdirAll wants a directory. The error
// must wrap with "mkdir parent" so operators can distinguish it from
// rename/chmod failures further down.
func TestSaveMkdirAllErrorSurfacesPath(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	// Plant a regular file at <base>/gum so MkdirAll(<base>/gum/<profile>)
	// trips on a non-directory ancestor.
	blocker := filepath.Join(base, "gum")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}

	c := &config.Config{Values: map[string]string{"output.profile.default": "human"}}
	err := config.Save("blocked", c)
	if err == nil {
		t.Fatalf("Save: nil error; want mkdir failure")
	}
	if !strings.Contains(err.Error(), "mkdir parent") {
		t.Errorf("err=%q; want mkdir-parent wrap", err)
	}
}
