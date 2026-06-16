package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCacheMigrateExplicitEmptyProfileFallsBackToDefault pins
// newCacheMigrateCmd's `profile == "" → profile = "default"` arm
// (cache.go:39-41). Reached when the caller passes `--profile=`
// explicitly (cobra accepts an empty string, distinct from the
// "default" default applied when the flag is absent). The migration
// must still resolve to .../gum/default/.
func TestCacheMigrateExplicitEmptyProfileFallsBackToDefault(t *testing.T) {
	root := withTempCacheRootCLI(t)
	_, err := runCLI(t, "--profile=", "cache", "migrate")
	if err != nil {
		t.Fatalf("gum --profile= cache migrate: %v", err)
	}
	wantDir := filepath.Join(root, "gum", "default")
	if _, statErr := os.Stat(wantDir); statErr != nil {
		t.Errorf("expected fallback profile dir %q to exist, stat err: %v", wantDir, statErr)
	}
}

// TestCacheMigrateUserHomeDirErrorPropagates pins
// newCacheMigrateCmd's `os.UserHomeDir err → return err` arm
// (cache.go:45-47). Reached when XDG_CACHE_HOME is empty AND every
// home-resolving env (HOME on unix, USERPROFILE on Windows) is empty
// so os.UserHomeDir returns an error.
func TestCacheMigrateUserHomeDirErrorPropagates(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", "")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", "")
		t.Setenv("HOMEDRIVE", "")
		t.Setenv("HOMEPATH", "")
	}

	_, err := runCLI(t, "cache", "migrate")
	if err == nil {
		t.Fatal("gum cache migrate (no HOME) err=nil; want UserHomeDir err propagation")
	}
}

// TestCacheMigrateMkdirAllErrorPropagates pins
// newCacheMigrateCmd's `os.MkdirAll err → return err` arm
// (cache.go:51-53). Reached by planting a regular file at the path
// MkdirAll would create, so the call fails with ENOTDIR.
func TestCacheMigrateMkdirAllErrorPropagates(t *testing.T) {
	root := t.TempDir()
	// Plant a regular file where MkdirAll wants a directory chain:
	// XDG_CACHE_HOME/gum is a file, so MkdirAll(.../gum/default) fails.
	if err := os.WriteFile(filepath.Join(root, "gum"), []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker file: %v", err)
	}
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := runCLI(t, "cache", "migrate")
	if err == nil {
		t.Fatal("gum cache migrate (blocked MkdirAll) err=nil; want ENOTDIR propagation")
	}
	if !strings.Contains(err.Error(), "not a directory") &&
		!strings.Contains(err.Error(), "ENOTDIR") {
		t.Logf("err=%v (accepting any non-nil error)", err)
	}
}
