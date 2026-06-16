package mcp

import (
	"testing"
)

// TestSetProfileEmptyFallsBackToDefault covers the empty-name guard:
// callers that pass "" must get the "default" profile bound, otherwise
// audit.broken sentinel resolution would walk the wrong path.
func TestSetProfileEmptyFallsBackToDefault(t *testing.T) {
	s := &Server{profile: "stale"}
	_ = s.SetProfile("")
	if s.profile != "default" {
		t.Errorf("profile=%q after SetProfile(\"\"); want default", s.profile)
	}
}

// TestSetProfileExplicitNameWins pins the happy path: explicit names
// round-trip verbatim so a per-profile audit dir stays addressable.
func TestSetProfileExplicitNameWins(t *testing.T) {
	s := &Server{}
	_ = s.SetProfile("teamA")
	if s.profile != "teamA" {
		t.Errorf("profile=%q; want teamA", s.profile)
	}
}

// TestCacheRootDirHonoursXDG: XDG_CACHE_HOME wins for path resolution.
func TestCacheRootDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/x/cache")
	got, err := cacheRootDir()
	if err != nil {
		t.Fatalf("cacheRootDir: %v", err)
	}
	if got != "/x/cache/gum" {
		t.Errorf("got=%q; want /x/cache/gum", got)
	}
}

// TestCacheRootDirHomeFallback drives the $HOME branch when
// XDG_CACHE_HOME is empty — the helper must derive ~/.cache/gum.
func TestCacheRootDirHomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", home)
	got, err := cacheRootDir()
	if err != nil {
		t.Fatalf("cacheRootDir: %v", err)
	}
	want := home + "/.cache/gum"
	if got != want {
		t.Errorf("got=%q; want %q", got, want)
	}
}
