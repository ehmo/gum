package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGUMProfileEnvironmentSelectsActiveProfile(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("GUM_PROFILE", "envprof")

	if _, err := runCLI(t, "config", "set", "output.default_format=json"); err != nil {
		t.Fatalf("gum config set with GUM_PROFILE: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configRoot, "gum", "envprof", "config.toml")); err != nil {
		t.Fatalf("env profile config not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configRoot, "gum", "default", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("default profile was touched with GUM_PROFILE set; stat err=%v", err)
	}
}

func TestProfileFlagOverridesGUMProfileEnvironment(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("GUM_PROFILE", "envprof")

	if _, err := runCLI(t, "--profile=flagprof", "config", "set", "output.default_format=json"); err != nil {
		t.Fatalf("gum --profile flagprof config set: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configRoot, "gum", "flagprof", "config.toml")); err != nil {
		t.Fatalf("flag profile config not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configRoot, "gum", "envprof", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("env profile was touched despite --profile override; stat err=%v", err)
	}
}

func TestInvalidProfileRejectedBeforeFilesystemTouch(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	cacheRoot := filepath.Join(root, "cache")
	configRoot := filepath.Join(root, "config")
	t.Setenv("XDG_DATA_HOME", dataRoot)
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	t.Setenv("XDG_CONFIG_HOME", configRoot)

	commands := [][]string{
		{"--profile=../escape", "cache", "clear", "--bak"},
		{"--profile=../escape", "catalog", "list-overrides"},
		{"--profile=../escape", "doctor", "--format=json"},
		{"--profile=../escape", "plugin", "install", "/no/such/path"},
		{"--profile=../escape", "mcp", "--stdio"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args[1:], "_"), func(t *testing.T) {
			out, err := runCLI(t, args...)
			if err == nil {
				t.Fatalf("runCLI(%v)=nil err, stdout=%q; want invalid profile error", args, out)
			}
			if !strings.Contains(err.Error(), "invalid profile name") {
				t.Fatalf("runCLI(%v) err=%v, want invalid profile name", args, err)
			}
		})
	}

	for _, escaped := range []string{
		filepath.Join(dataRoot, "escape"),
		filepath.Join(cacheRoot, "escape"),
		filepath.Join(configRoot, "escape"),
	} {
		if _, err := os.Stat(escaped); !os.IsNotExist(err) {
			t.Fatalf("escaped path %s was touched; stat err=%v", escaped, err)
		}
	}
}
