package profile_test

// resolver_test.go — Red team tests for gum-np38.8 (§9.2 three-level fallback).
//
// Fixture convention: XDG_CONFIG_HOME is set to <tmp>/home_config, so the
// user-global profile path is <tmp>/home_config/gum/profiles/<name>.toml.
// Green's ResolveProfile MUST honour XDG_CONFIG_HOME when constructing the
// user-global path. Callers who use os.UserHomeDir instead MUST switch to
// XDG_CONFIG_HOME or this test will fail.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// buildFixtures creates the standard fixture tree under tmp and returns tmp.
//
//	<tmp>/
//	  project/
//	    .gum/
//	      profiles/
//	        gmail_list.toml   -> default_format = "toon"; sort_by = "id"
//	    sub/
//	      deep/               -> for upward-walk tests
//	  home_config/
//	    gum/
//	      profiles/
//	        gmail_list.toml   -> sort_by = "userglobal"
//	        only_user.toml    -> default_format = "json"
func buildFixtures(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()

	// Project-local gmail_list
	projectProfiles := filepath.Join(tmp, "project", ".gum", "profiles")
	if err := os.MkdirAll(projectProfiles, 0o755); err != nil {
		t.Fatalf("mkdir project profiles: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(projectProfiles, "gmail_list.toml"),
		[]byte("default_format = \"toon\"\nsort_by = \"id\"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write project gmail_list.toml: %v", err)
	}

	// Sub-directory for upward-walk test
	if err := os.MkdirAll(filepath.Join(tmp, "project", "sub", "deep"), 0o755); err != nil {
		t.Fatalf("mkdir sub/deep: %v", err)
	}

	// User-global profiles
	userProfiles := filepath.Join(tmp, "home_config", "gum", "profiles")
	if err := os.MkdirAll(userProfiles, 0o755); err != nil {
		t.Fatalf("mkdir user profiles: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(userProfiles, "gmail_list.toml"),
		[]byte("sort_by = \"userglobal\"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write user gmail_list.toml: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(userProfiles, "only_user.toml"),
		[]byte("default_format = \"json\"\n"),
		0o644,
	); err != nil {
		t.Fatalf("write only_user.toml: %v", err)
	}

	return tmp
}

// noCatalog is a catalog-lookup stub that always returns (nil, false).
func noCatalog(_ string) (*profile.Profile, bool) { return nil, false }

// TestResolveProfileProjectLocalWins verifies that a project-local profile is
// returned when rootPath points directly to the project root.
func TestResolveProfileProjectLocalWins(t *testing.T) {
	tmp := buildFixtures(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "home_config"))

	rootPath := filepath.Join(tmp, "project")
	p, src, err := profile.ResolveProfile(rootPath, "gmail_list", noCatalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != profile.SourceProjectLocal {
		t.Errorf("source = %q, want %q", src, profile.SourceProjectLocal)
	}
	if p.SortBy != "id" {
		t.Errorf("SortBy = %q, want \"id\"", p.SortBy)
	}
}

// TestResolveProfileWalksUpward verifies that ResolveProfile walks ancestor
// directories from rootPath until it finds a directory containing .gum/.
func TestResolveProfileWalksUpward(t *testing.T) {
	tmp := buildFixtures(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "home_config"))

	// rootPath is two levels below the .gum/ directory.
	rootPath := filepath.Join(tmp, "project", "sub", "deep")
	p, src, err := profile.ResolveProfile(rootPath, "gmail_list", noCatalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != profile.SourceProjectLocal {
		t.Errorf("source = %q, want %q", src, profile.SourceProjectLocal)
	}
	if p.SortBy != "id" {
		t.Errorf("SortBy = %q, want \"id\" (found via upward walk)", p.SortBy)
	}
}

// TestResolveProfileFallsBackToUserGlobal verifies that a profile absent from
// the project-local layer is found in the user-global layer.
func TestResolveProfileFallsBackToUserGlobal(t *testing.T) {
	tmp := buildFixtures(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "home_config"))

	rootPath := filepath.Join(tmp, "project")
	p, src, err := profile.ResolveProfile(rootPath, "only_user", noCatalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != profile.SourceUserGlobal {
		t.Errorf("source = %q, want %q", src, profile.SourceUserGlobal)
	}
	if p.DefaultFormat != "json" {
		t.Errorf("DefaultFormat = %q, want \"json\"", p.DefaultFormat)
	}
}

// TestResolveProfileFallsBackToCatalogEmbedded verifies that a profile absent
// from both project-local and user-global layers is returned from the catalog
// callback.
func TestResolveProfileFallsBackToCatalogEmbedded(t *testing.T) {
	tmp := buildFixtures(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "home_config"))

	catalogProfile := &profile.Profile{
		DefaultFormat: "toon",
		SortBy:        "catalogembedded",
	}
	catalogLookup := func(name string) (*profile.Profile, bool) {
		if name == "catalog_only" {
			return catalogProfile, true
		}
		return nil, false
	}

	rootPath := filepath.Join(tmp, "project")
	p, src, err := profile.ResolveProfile(rootPath, "catalog_only", catalogLookup)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != profile.SourceCatalogEmbedded {
		t.Errorf("source = %q, want %q", src, profile.SourceCatalogEmbedded)
	}
	if p.SortBy != "catalogembedded" {
		t.Errorf("SortBy = %q, want \"catalogembedded\"", p.SortBy)
	}
}

// TestResolveProfileEmptyRootPathSkipsProjectLocal verifies that passing an
// empty rootPath disables project-local lookup entirely (must not fall back to
// $PWD) and falls through to the user-global layer.
func TestResolveProfileEmptyRootPathSkipsProjectLocal(t *testing.T) {
	tmp := buildFixtures(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "home_config"))

	// rootPath is empty — project-local lookup must be disabled.
	p, src, err := profile.ResolveProfile("", "gmail_list", noCatalog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != profile.SourceUserGlobal {
		t.Errorf("source = %q, want %q (project-local must be skipped)", src, profile.SourceUserGlobal)
	}
	if p.SortBy != "userglobal" {
		t.Errorf("SortBy = %q, want \"userglobal\"", p.SortBy)
	}
}

// TestResolveProfileNotFoundReturnsSentinel verifies that ErrProfileNotFound
// is returned when no resolution layer supplies the requested profile.
func TestResolveProfileNotFoundReturnsSentinel(t *testing.T) {
	tmp := buildFixtures(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "home_config"))

	_, _, err := profile.ResolveProfile("", "nonexistent_profile", noCatalog)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, profile.ErrProfileNotFound) {
		t.Errorf("err = %v; want errors.Is(err, profile.ErrProfileNotFound) == true", err)
	}
}

// TestResolutionSourceStringConstants verifies that the three ResolutionSource
// constants carry the exact string values required by the gum describe
// output_profile_source field (spec §2073).
func TestResolutionSourceStringConstants(t *testing.T) {
	tests := []struct {
		constant profile.ResolutionSource
		want     string
	}{
		{profile.SourceProjectLocal, "project-local"},
		{profile.SourceUserGlobal, "user-global"},
		{profile.SourceCatalogEmbedded, "catalog-embedded"},
	}
	for _, tc := range tests {
		if string(tc.constant) != tc.want {
			t.Errorf("ResolutionSource constant = %q, want %q", string(tc.constant), tc.want)
		}
	}
}

// TestResolveProfileUserGlobalFallsBackToHome pins the lookupUserGlobal
// fallback branch: with XDG_CONFIG_HOME unset, the resolver MUST look
// under $HOME/.config/gum/profiles. A regression that always reaches
// for XDG_CONFIG_HOME would silently miss user profiles on hosts where
// only HOME is defined.
func TestResolveProfileUserGlobalFallsBackToHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", tmp)

	profDir := filepath.Join(tmp, ".config", "gum", "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "home_only.toml"),
		[]byte("default_format = \"toon\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	p, src, err := profile.ResolveProfile("", "home_only", noCatalog)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if src != profile.SourceUserGlobal {
		t.Errorf("source=%q; want user-global", src)
	}
	if p.DefaultFormat != "toon" {
		t.Errorf("DefaultFormat=%q; want toon", p.DefaultFormat)
	}
}

// TestResolveProfileUserGlobalParseErrorSurfaces pins the (nil, true, err)
// branch: a syntactically-broken user-global TOML must surface the parser
// error verbatim so users see a clear "edit your file" diagnostic instead
// of an opaque "not found" sentinel.
func TestResolveProfileUserGlobalParseErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	profDir := filepath.Join(tmp, "gum", "profiles")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "broken.toml"),
		[]byte("default_format = [unterminated\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, err := profile.ResolveProfile("", "broken", noCatalog)
	if err == nil {
		t.Fatal("want parse error; got nil")
	}
	if errors.Is(err, profile.ErrProfileNotFound) {
		t.Errorf("err=%v; must NOT collapse to ErrProfileNotFound", err)
	}
}

// TestResolveProfileUserGlobalReadErrorSurfaces pins the
// "ReadFile returns non-NotExist error" branch: planting a *directory*
// at the .toml candidate path makes ReadFile return EISDIR, which must
// propagate (not be swallowed as "not found").
func TestResolveProfileUserGlobalReadErrorSurfaces(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	// Plant a directory at the candidate path.
	candidateAsDir := filepath.Join(tmp, "gum", "profiles", "blocker.toml")
	if err := os.MkdirAll(candidateAsDir, 0o755); err != nil {
		t.Fatalf("mkdir-as-file: %v", err)
	}

	_, _, err := profile.ResolveProfile("", "blocker", noCatalog)
	if err == nil {
		t.Fatal("want EISDIR-style error; got nil")
	}
	if errors.Is(err, profile.ErrProfileNotFound) {
		t.Errorf("err=%v; must NOT collapse to ErrProfileNotFound", err)
	}
	if os.IsNotExist(err) {
		t.Errorf("err=%v; must NOT be a NotExist error", err)
	}
}
