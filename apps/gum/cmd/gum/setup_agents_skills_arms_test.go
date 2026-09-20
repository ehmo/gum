package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	skillreg "github.com/ehmo/gum/internal/skills"
)

// blockedHome returns a HOME path whose parent is a regular file, so any
// MkdirAll under it fails with ENOTDIR.
func blockedHome(t *testing.T) string {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	return filepath.Join(blocker, "home")
}

func TestSplitCSVRejectsBlankInput(t *testing.T) {
	if got := splitCSV("   "); got != nil {
		t.Fatalf("got %#v; want nil", got)
	}
	if got := splitCSV("a, ,b"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("got %#v; want [a b]", got)
	}
}

func TestPrintSkillsWrittenUsesTheDryRunVerb(t *testing.T) {
	var buf bytes.Buffer
	printSkillsWritten(&buf, []string{"gum/SKILL.md"}, true)
	if !strings.Contains(buf.String(), "would write gum/SKILL.md") {
		t.Fatalf("output %q; want the would-write verb", buf.String())
	}
}

func TestResolveSkillsInstallDirArms(t *testing.T) {
	t.Run("explicit dir wins", func(t *testing.T) {
		got, err := resolveSkillsInstallDir("codex", "/tmp/skills")
		if err != nil || got != "/tmp/skills" {
			t.Fatalf("got (%q,%v); want (/tmp/skills,nil)", got, err)
		}
	})

	t.Run("CODEX_HOME", func(t *testing.T) {
		t.Setenv("CODEX_HOME", "/opt/codex")
		got, err := resolveSkillsInstallDir("codex", "")
		if err != nil || got != filepath.Join("/opt/codex", "skills") {
			t.Fatalf("got (%q,%v); want /opt/codex/skills", got, err)
		}
	})

	t.Run("home fallback", func(t *testing.T) {
		t.Setenv("CODEX_HOME", "")
		t.Setenv("HOME", "/home/tester")
		got, err := resolveSkillsInstallDir("codex", "")
		if err != nil || got != filepath.Join("/home/tester", ".codex", "skills") {
			t.Fatalf("got (%q,%v); want /home/tester/.codex/skills", got, err)
		}
	})

	t.Run("no home", func(t *testing.T) {
		t.Setenv("CODEX_HOME", "")
		t.Setenv("HOME", "")
		_, err := resolveSkillsInstallDir("codex", "")
		if err == nil || !strings.Contains(err.Error(), "resolve codex skills directory") {
			t.Fatalf("got %v; want a resolve-directory error", err)
		}
	})

	t.Run("unsupported target", func(t *testing.T) {
		_, err := resolveSkillsInstallDir("claude", "")
		if err == nil || !strings.Contains(err.Error(), "unsupported skills target") {
			t.Fatalf("got %v; want an unsupported-target error", err)
		}
	})
}

func TestWriteInstallableSkillsArms(t *testing.T) {
	installables := skillreg.DefaultInstallableSkills()
	if len(installables) == 0 {
		t.Fatal("no installable skills embedded")
	}

	t.Run("blank base", func(t *testing.T) {
		if _, err := writeInstallableSkills("  ", installables, false, false); err == nil {
			t.Fatal("blank base: want an error")
		}
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		base := t.TempDir()
		written, err := writeInstallableSkills(base, installables, false, true)
		if err != nil || len(written) == 0 {
			t.Fatalf("got (%v,%v); want the planned paths", written, err)
		}
		entries, rerr := os.ReadDir(base)
		if rerr != nil || len(entries) != 0 {
			t.Fatalf("dry run touched disk: %v %v", entries, rerr)
		}
	})

	t.Run("writes then refuses to clobber", func(t *testing.T) {
		base := t.TempDir()
		written, err := writeInstallableSkills(base, installables, false, false)
		if err != nil || len(written) == 0 {
			t.Fatalf("first write: got (%v,%v)", written, err)
		}
		if _, serr := os.Stat(filepath.Join(base, written[0])); serr != nil {
			t.Fatalf("stat %s: %v", written[0], serr)
		}
		_, err = writeInstallableSkills(base, installables, false, false)
		if err == nil || !strings.Contains(err.Error(), "pass --force to overwrite") {
			t.Fatalf("second write: got %v; want the clobber refusal", err)
		}
		if _, err := writeInstallableSkills(base, installables, true, false); err != nil {
			t.Fatalf("forced write: %v", err)
		}
	})

	t.Run("invalid skill directory", func(t *testing.T) {
		bad := []skillreg.InstallableSkill{{
			Directory: "../escape",
			Files:     []skillreg.InstallableFile{{Path: "SKILL.md", Contents: "x"}},
		}}
		if _, err := writeInstallableSkills(t.TempDir(), bad, false, false); err == nil {
			t.Fatal("want an invalid-directory error")
		}
	})

	t.Run("unwritable base", func(t *testing.T) {
		if _, err := writeInstallableSkills(blockedHome(t), installables, false, false); err == nil {
			t.Fatal("want a mkdir error")
		}
	})

	t.Run("skill directory cannot be created", func(t *testing.T) {
		base := t.TempDir()
		t.Cleanup(func() { _ = os.Chmod(base, 0o700) })
		if err := os.Chmod(base, 0o500); err != nil {
			t.Fatalf("chmod base: %v", err)
		}
		if _, err := writeInstallableSkills(base, installables, false, false); err == nil {
			t.Fatal("want a mkdir error for a read-only base")
		}
	})

	t.Run("skill file cannot be written", func(t *testing.T) {
		base := t.TempDir()
		plan, err := writeInstallableSkills(base, installables, false, true)
		if err != nil || len(plan) == 0 {
			t.Fatalf("dry run: got (%v,%v)", plan, err)
		}
		// The directory exists, so MkdirAll succeeds and the write itself is
		// what fails.
		dir := filepath.Dir(filepath.Join(base, plan[0]))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("chmod %s: %v", dir, err)
		}
		if _, err := writeInstallableSkills(base, installables, false, false); err == nil {
			t.Fatal("want a write error for a read-only skill directory")
		}
	})
}

func TestSkillsListAndShowText(t *testing.T) {
	list := newSkillsListCmd()
	var out bytes.Buffer
	list.SetOut(&out)
	list.SetErr(&out)
	list.SetArgs(nil)
	if err := list.Execute(); err != nil {
		t.Fatalf("skills list: %v", err)
	}
	if !strings.Contains(out.String(), "sha256=") {
		t.Fatalf("list output %q; want the sha256 column", out.String())
	}
	name := strings.Fields(out.String())[0]

	show := newSkillsShowCmd()
	var shown bytes.Buffer
	show.SetOut(&shown)
	show.SetErr(&shown)
	show.SetArgs([]string{name})
	if err := show.Execute(); err != nil {
		t.Fatalf("skills show: %v", err)
	}
	if !strings.Contains(shown.String(), "name: "+name) {
		t.Fatalf("show output %q; want the name header", shown.String())
	}

	missing := newSkillsShowCmd()
	missing.SetOut(&bytes.Buffer{})
	missing.SetErr(&bytes.Buffer{})
	missing.SetArgs([]string{"no-such-skill"})
	if err := missing.Execute(); err == nil {
		t.Fatal("skills show no-such-skill: want an error")
	}
}

func TestSkillsExportArms(t *testing.T) {
	t.Run("text output", func(t *testing.T) {
		base := t.TempDir()
		cmd := newSkillsExportCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"--out", base})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("skills export: %v", err)
		}
		if !strings.Contains(out.String(), "wrote ") {
			t.Fatalf("output %q; want the wrote lines", out.String())
		}
	})

	t.Run("write error", func(t *testing.T) {
		cmd := newSkillsExportCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--out", blockedHome(t)})
		if err := cmd.Execute(); err == nil {
			t.Fatal("want a write error")
		}
	})
}

func TestSkillsInstallArms(t *testing.T) {
	t.Run("json output", func(t *testing.T) {
		base := t.TempDir()
		cmd := newSkillsInstallCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"--dir", base, "--format", "json"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("skills install: %v", err)
		}
		if !strings.Contains(out.String(), `"target": "codex"`) {
			t.Fatalf("output %q; want the codex target", out.String())
		}
	})

	t.Run("write error", func(t *testing.T) {
		cmd := newSkillsInstallCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--dir", blockedHome(t)})
		if err := cmd.Execute(); err == nil {
			t.Fatal("want a write error")
		}
	})

	t.Run("resolve error", func(t *testing.T) {
		cmd := newSkillsInstallCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--target", "claude"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("want an unsupported-target error")
		}
	})
}

func TestAgentsInstallArms(t *testing.T) {
	t.Run("no home", func(t *testing.T) {
		t.Setenv("HOME", "")
		cmd := newAgentsInstallCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "resolve home directory") {
			t.Fatalf("got %v; want a resolve-home error", err)
		}
	})

	t.Run("invalid target", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cmd := newAgentsInstallCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--target", "emacs"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("want an invalid-target error")
		}
	})

	t.Run("text plan", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cmd := newAgentsInstallCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"--target", "codex", "--dry-run"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("agents install: %v", err)
		}
		if !strings.Contains(out.String(), "would_write") {
			t.Fatalf("output %q; want the would_write plan", out.String())
		}
	})
}

func TestSetupArms(t *testing.T) {
	t.Run("no home", func(t *testing.T) {
		t.Setenv("HOME", "")
		cmd := newSetupCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--yes"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "resolve home directory") {
			t.Fatalf("got %v; want a resolve-home error", err)
		}
	})

	t.Run("invalid target", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cmd := newSetupCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--yes", "--target", "emacs"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("want an invalid-target error")
		}
	})

	t.Run("dry run text", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		cmd := newSetupCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"--dry-run", "--target", "codex"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("setup --dry-run: %v", err)
		}
		if !strings.Contains(out.String(), "dry run complete") {
			t.Fatalf("output %q; want the dry-run footer", out.String())
		}
	})

	// The plan is built with DryRun:true and succeeds; the apply pass is what
	// fails once the home directory cannot be created.
	t.Run("apply failure", func(t *testing.T) {
		t.Setenv("HOME", blockedHome(t))
		cmd := newSetupCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--yes", "--target", "codex", "--format", "json"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("want the apply error")
		}
	})
}
