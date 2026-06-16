package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCacheClearBakExplicitEmptyProfileFallsBack pins
// newCacheClearCmd's `profile == "" → profile = "default"` arm
// (cache.go:156-158). Reached when --profile= is passed explicitly
// AND --bak is set so the code progresses past the no-flag short-
// circuit and into the profile-resolution block.
func TestCacheClearBakExplicitEmptyProfileFallsBack(t *testing.T) {
	root := withTempCacheRootCLI(t)
	// No http.db.bak exists, so removed=false but path must point at
	// the .../gum/default/http.db.bak fallback.
	out, err := runCLI(t, "--profile=", "cache", "clear", "--bak")
	if err != nil {
		t.Fatalf("gum --profile= cache clear --bak: %v", err)
	}
	var result map[string]any
	if jerr := json.Unmarshal([]byte(out), &result); jerr != nil {
		t.Fatalf("stdout not JSON: %v\n%s", jerr, out)
	}
	wantPath := filepath.Join(root, "gum", "default", "http.db.bak")
	if got, _ := result["path"].(string); got != wantPath {
		t.Errorf("path=%q; want %q (default fallback)", got, wantPath)
	}
}

// TestCacheClearBakUserHomeDirErrorPropagates pins
// newCacheClearCmd's UserHomeDir err arm (cache.go:161-165).
// XDG_CACHE_HOME empty + HOME empty so UserHomeDir errs.
func TestCacheClearBakUserHomeDirErrorPropagates(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", "")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", "")
		t.Setenv("HOMEDRIVE", "")
		t.Setenv("HOMEPATH", "")
	}
	_, err := runCLI(t, "cache", "clear", "--bak")
	if err == nil {
		t.Fatal("gum cache clear --bak (no HOME) err=nil; want UserHomeDir propagation")
	}
}

// TestCacheClearBakStatErrorOtherThanNotExist pins newCacheClearCmd's
// `else if !errors.Is(err, os.ErrNotExist) → return err` arm
// (cache.go:180-182). Reached when os.Stat(bakPath) returns an error
// that isn't IsNotExist — here the parent directory is chmod-stripped
// so Stat fails with EACCES. Skipped under euid 0.
func TestCacheClearBakStatErrorOtherThanNotExist(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("EACCES not surfaced when running as root")
	}
	root := t.TempDir()
	profileDir := filepath.Join(root, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll profile: %v", err)
	}
	// Plant the bak file so it exists, then strip perms from parent.
	bakPath := filepath.Join(profileDir, "http.db.bak")
	if err := os.WriteFile(bakPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant bak: %v", err)
	}
	if err := os.Chmod(profileDir, 0o000); err != nil {
		t.Fatalf("chmod profile dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(profileDir, 0o755) })

	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := runCLI(t, "cache", "clear", "--bak")
	if err == nil {
		t.Fatal("gum cache clear --bak (EACCES parent) err=nil; want stat err propagation")
	}
}
