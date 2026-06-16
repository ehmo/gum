package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoctorPluginListFailureSurfacesNonOK pins the error arm of
// doctorPlugin: when the plugin host's List() returns a real error
// (NOT IsNotExist), the check MUST return OK=false with the underlying
// error message in Hint so an operator can diagnose without -v logging.
//
// To force this, we plant a regular file at the default install root
// path (~/.local/share/gum/plugins) so os.ReadDir returns ENOTDIR — a
// not-IsNotExist error that DOES propagate out of List(). The
// "no install root yet" IsNotExist arm returns nil and would hit the
// OK=true branch instead.
func TestDoctorPluginListFailureSurfacesNonOK(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	pluginsParent := filepath.Join(tmpHome, ".local", "share", "gum")
	if err := os.MkdirAll(pluginsParent, 0o755); err != nil {
		t.Fatal(err)
	}
	// "plugins" is a regular file, not a directory — ENOTDIR on read.
	if err := os.WriteFile(filepath.Join(pluginsParent, "plugins"),
		[]byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := doctorPlugin()
	if got.Name != "plugin" {
		t.Errorf("Name=%q; want plugin", got.Name)
	}
	if got.OK {
		t.Errorf("OK=true; want false on ENOTDIR list failure: %+v", got)
	}
	if got.Hint == "" {
		t.Errorf("Hint empty; want underlying list error echoed")
	}
	if !strings.Contains(got.Summary, "plugin host failed") {
		t.Errorf("Summary=%q; want 'plugin host failed'", got.Summary)
	}
}
