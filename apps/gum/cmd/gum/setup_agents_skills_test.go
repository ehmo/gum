package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupDryRunJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"setup", "--target", "codex", "--features", "skills,mcp", "--dry-run", "--format", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v stderr=%q", err, stderr.String())
	}
	var payload setupResult
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("setup json = %q err=%v", stdout.String(), err)
	}
	if payload.Request.Target != "codex" || !payload.Request.DryRun || len(payload.AgentPlan.Actions) == 0 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestSetupAppliesWithYes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"setup", "--target", "codex", "--features", "mcp", "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "setup complete") || !strings.Contains(stdout.String(), "gum doctor") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatalf("codex mcp config: %v", err)
	}
}

func TestSetupCancelledWithoutYes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetIn(strings.NewReader("\n"))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"setup", "--target", "codex", "--features", "mcp"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "setup cancelled") {
		t.Fatalf("err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
}

func TestAgentsInstallProjectDryRunJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"agents", "install", "--target", "cursor", "--scope", "project", "--dry-run", "--format", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"target": "cursor"`) || !strings.Contains(stdout.String(), `"dry_run": true`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSkillsCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"skills", "list", "--format", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("skills list: %v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"name": "core"`) || strings.Contains(stdout.String(), "body") {
		t.Fatalf("skills list stdout=%q", stdout.String())
	}

	root = newRootCmd()
	stdout.Reset()
	stderr.Reset()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"skills", "show", "mcp", "--format", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("skills show: %v stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "gum mcp") {
		t.Fatalf("skills show stdout=%q", stdout.String())
	}
}

func TestSkillsInstallWritesCodexHome(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"skills", "install", "--force"})
	if err := root.Execute(); err != nil {
		t.Fatalf("skills install: %v stderr=%q", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(codexHome, "skills", "gum-mcp", "agents", "openai.yaml")); err != nil {
		t.Fatalf("installed skill file: %v", err)
	}
}

func TestSkillsExportAndErrors(t *testing.T) {
	outDir := t.TempDir()
	root := newRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"skills", "export", "--out", outDir, "--format", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("skills export: %v stderr=%q", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(outDir, "gum", "SKILL.md")); err != nil {
		t.Fatalf("exported skill: %v", err)
	}

	root = newRootCmd()
	stdout.Reset()
	stderr.Reset()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"skills", "export"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--out is required") {
		t.Fatalf("missing --out err=%v", err)
	}

	root = newRootCmd()
	stdout.Reset()
	stderr.Reset()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"skills", "install", "--target", "other", "--dry-run"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unsupported skills target") {
		t.Fatalf("bad target err=%v", err)
	}
}

func TestSetupHelpers(t *testing.T) {
	if got := strings.Join(splitCSV(" skills, mcp ,, "), "|"); got != "skills|mcp" {
		t.Fatalf("splitCSV=%q", got)
	}
	if normalizedTarget("") != "all" || normalizedScope("") != "user" || normalizedToolset("") != "core" || normalizedSkillsTarget("") != "codex" {
		t.Fatal("normalization defaults changed")
	}
	if got := normalizedFeatures(nil); len(got) != 2 || got[0] != "skills" || got[1] != "mcp" {
		t.Fatalf("normalizedFeatures=%#v", got)
	}
	if _, err := installableRelativePath(".bad", "SKILL.md"); err == nil {
		t.Fatal("invalid installable dir err=nil")
	}
	if _, err := installableRelativePath("gum", "../SKILL.md"); err == nil {
		t.Fatal("invalid installable path err=nil")
	}
}
