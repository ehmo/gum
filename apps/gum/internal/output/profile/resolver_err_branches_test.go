package profile_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// TestResolveProfileProjectLocalParseErrorPropagates pins
// ResolveProfile's `lookupProjectLocal err → return nil, "", err` arm
// (resolver.go:36-38) AND lookupProjectLocal's `Parse err → return
// nil, true, err` arm (resolver.go:69-71). A syntactically-broken
// project-local TOML MUST surface the parse err — without this guard
// resolution would silently fall through to user-global and the user
// would never know their project profile is invalid.
//
// Single test exercises both arms: lookupProjectLocal returns
// (nil, true, parseErr), and ResolveProfile's branch propagates without
// re-wrapping.
func TestResolveProfileProjectLocalParseErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	projDir := filepath.Join(tmp, "project", ".gum", "profiles")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Unknown key — parser rejects it at line 1.
	if err := os.WriteFile(filepath.Join(projDir, "broken.toml"),
		[]byte("unknown_key = \"bad\"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, err := profile.ResolveProfile(filepath.Join(tmp, "project"), "broken", noCatalog)
	if err == nil {
		t.Fatal("ResolveProfile(broken project-local)=nil err; want parse-err propagation")
	}
	if errors.Is(err, profile.ErrProfileNotFound) {
		t.Errorf("err=%v; must NOT collapse to ErrProfileNotFound (would hide the broken project file)", err)
	}
}

// TestResolveProfileProjectLocalReadErrorPropagates pins
// lookupProjectLocal's `!os.IsNotExist(err) → return nil, false, err`
// arm (resolver.go:73-75). Reached when the candidate .toml path is
// a DIRECTORY rather than a regular file → os.ReadFile returns EISDIR,
// which is NOT a NotExist err. Without this propagation a broken
// filesystem state would silently fall through to user-global, masking
// the env-misconfig.
func TestResolveProfileProjectLocalReadErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	// Plant a directory at the candidate file path.
	candidateAsDir := filepath.Join(tmp, "project", ".gum", "profiles", "blocker.toml")
	if err := os.MkdirAll(candidateAsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, _, err := profile.ResolveProfile(filepath.Join(tmp, "project"), "blocker", noCatalog)
	if err == nil {
		t.Fatal("ResolveProfile(EISDIR project-local)=nil err; want raw read-err propagation")
	}
	if errors.Is(err, profile.ErrProfileNotFound) {
		t.Errorf("err=%v; must NOT collapse to ErrProfileNotFound", err)
	}
	if os.IsNotExist(err) {
		t.Errorf("err=%v; must NOT be a NotExist err (EISDIR is the signal of bad env state)", err)
	}
}

// TestResolveProfileUserGlobalHomeUnavailableFallsThroughSilently pins
// lookupUserGlobal's `os.UserHomeDir err → return nil, false, nil`
// arm (resolver.go:91-94). With both XDG_CONFIG_HOME and HOME unset,
// UserHomeDir fails — but the lookupUserGlobal contract says this is
// NOT an error: it just means "no user-global profile available",
// so the resolver falls through to the catalog layer silently.
//
// Without this guard, a host with no HOME (e.g., a misconfigured
// container) would surface a UserHomeDir err to the operator even
// when the catalog layer would have served the profile.
//
// Skipped on Windows: USERPROFILE has different semantics and
// UserHomeDir doesn't depend on HOME there.
func TestResolveProfileUserGlobalHomeUnavailableFallsThroughSilently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UserHomeDir on Windows uses USERPROFILE, not HOME; HOME=\"\" trick doesn't apply")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	// Catalog stub returns a sentinel — proves the resolver passed
	// THROUGH user-global silently rather than erroring out at it.
	want := &profile.Profile{DefaultFormat: "toon"}
	catalogLookup := func(name string) (*profile.Profile, bool) {
		if name == "needs_catalog" {
			return want, true
		}
		return nil, false
	}

	got, src, err := profile.ResolveProfile("", "needs_catalog", catalogLookup)
	if err != nil {
		t.Fatalf("ResolveProfile(HOME='')=%v; want silent fall-through to catalog", err)
	}
	if src != profile.SourceCatalogEmbedded {
		t.Errorf("src=%q; want catalog-embedded (proves user-global silently bailed)", src)
	}
	if got != want {
		t.Errorf("got=%+v; want catalog Profile (resolver shouldn't substitute)", got)
	}
}
