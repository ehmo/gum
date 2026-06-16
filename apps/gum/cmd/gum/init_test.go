package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitWriteGUMmdAndPatchSettings exercises the `gum init --yes` path
// end-to-end by invoking the cobra command directly. Project-local mode is
// used to keep the test inside t.TempDir().
func TestInitWriteGUMmdAndPatchSettings(t *testing.T) {
	root := newRootCmd()

	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Run with --yes (no prompt) in project-local mode (default).
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"init", "--yes"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --yes: %v\nstdout:\n%s", err, out.String())
	}

	// settings.json must exist with mcpServers.gum entry.
	settings := filepath.Join(dir, ".claude", "settings.json")
	raw, err := os.ReadFile(settings)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("settings not JSON: %v\nraw:\n%s", err, raw)
	}
	servers, _ := top["mcpServers"].(map[string]any)
	gum, _ := servers["gum"].(map[string]any)
	if gum["command"] != "gum" {
		t.Errorf("settings.mcpServers.gum.command = %v; want gum", gum["command"])
	}

	// GUM.md must exist and carry the gum surface section.
	gumMD := filepath.Join(dir, "GUM.md")
	body, err := os.ReadFile(gumMD)
	if err != nil {
		t.Fatalf("read GUM.md: %v", err)
	}
	if !strings.Contains(string(body), "gum.search_apis") {
		head := len(body)
		if head > 400 {
			head = 400
		}
		t.Errorf("GUM.md missing tool-surface section; body head:\n%s", body[:head])
	}
}

// TestInitRefreshOnlyTouchesGUMmd verifies that `--refresh` rewrites GUM.md
// without modifying settings.json (in this case, never creating one).
func TestInitRefreshOnlyTouchesGUMmd(t *testing.T) {
	root := newRootCmd()

	dir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"init", "--refresh"})

	if err := root.Execute(); err != nil {
		t.Fatalf("init --refresh: %v\noutput:\n%s", err, out.String())
	}

	if _, err := os.ReadFile(filepath.Join(dir, "GUM.md")); err != nil {
		t.Errorf("GUM.md not written by --refresh: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); err == nil {
		t.Errorf("--refresh created settings.json; should leave it untouched")
	}
}

