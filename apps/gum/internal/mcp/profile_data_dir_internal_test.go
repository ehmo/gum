package mcp

import (
	"path/filepath"
	"testing"
)

// TestProfileDataDirHonoursXDG: when XDG_DATA_HOME is set the join is
// <xdg>/gum/<profile>. Empty profile defaults to "default" so callers
// don't have to thread a fallback through the resource handler.
func TestProfileDataDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/var/xdg")
	s := &Server{profile: "teamA"}
	got := s.profileDataDir()
	want := filepath.Join("/var/xdg", "gum", "teamA")
	if got != want {
		t.Errorf("got=%q; want %q", got, want)
	}
}

// TestProfileDataDirEmptyProfileFallback: empty profile string folds
// to "default" — keeps spec §13 result-artifact URIs resolvable when
// the operator hasn't named a profile explicitly.
func TestProfileDataDirEmptyProfileFallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/var/xdg")
	s := &Server{}
	got := s.profileDataDir()
	want := filepath.Join("/var/xdg", "gum", "default")
	if got != want {
		t.Errorf("got=%q; want %q", got, want)
	}
}

// TestProfileDataDirHomeFallback: when XDG_DATA_HOME is empty the
// helper falls back to $HOME/.local/share/gum/<profile>. The HOME
// override is required so this test doesn't depend on the developer's
// home dir layout.
func TestProfileDataDirHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", home)
	s := &Server{profile: "p1"}
	got := s.profileDataDir()
	want := filepath.Join(home, ".local", "share", "gum", "p1")
	if got != want {
		t.Errorf("got=%q; want %q", got, want)
	}
}
