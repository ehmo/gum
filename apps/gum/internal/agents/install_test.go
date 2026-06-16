package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallDryRunPlansAllTargetsWithoutWriting(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	plan, err := Install(Options{HomeDir: home, WorkDir: work, DryRun: true})
	if err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}
	if !plan.DryRun || plan.Target != TargetAll || len(plan.Actions) == 0 {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created files: %v", err)
	}
}

func TestInstallProjectCursorWritesSkillsAndMCP(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	plan, err := Install(Options{
		Target:  TargetCursor,
		Scope:   ScopeProject,
		HomeDir: home,
		WorkDir: work,
		Force:   true,
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(plan.Actions) != 7 {
		t.Fatalf("actions len=%d want 7: %#v", len(plan.Actions), plan.Actions)
	}
	if _, err := os.Stat(filepath.Join(work, ".agents", "skills", "gum", "SKILL.md")); err != nil {
		t.Fatalf("skill not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, ".agents", "skills", "gum-hasp", "SKILL.md")); err != nil {
		t.Fatalf("hasp skill not written: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(work, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf("mcp config not written: %v", err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("mcp json: %v", err)
	}
	server := cfg.MCPServers["gum"]
	if server.Command != "gum" || strings.Join(server.Args, " ") != "mcp --stdio" {
		t.Fatalf("server = %#v", server)
	}
}

func TestInstallRejectsSymlinkParent(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	real := filepath.Join(home, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(home, ".agents")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	_, err := Install(Options{Target: TargetCodex, Features: []string{FeatureSkills}, HomeDir: home, WorkDir: work})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Install symlink err=%v", err)
	}
}

func TestInstallRejectsInvalidOptions(t *testing.T) {
	if _, err := Install(Options{Target: "bad", HomeDir: t.TempDir(), WorkDir: t.TempDir()}); err == nil {
		t.Fatal("bad target err=nil")
	}
	if _, err := Install(Options{Features: []string{"bad"}, HomeDir: t.TempDir(), WorkDir: t.TempDir()}); err == nil {
		t.Fatal("bad feature err=nil")
	}
	if _, err := Install(Options{MCPArgs: []string{"bad\narg"}, HomeDir: t.TempDir(), WorkDir: t.TempDir()}); err == nil {
		t.Fatal("bad mcp arg err=nil")
	}
}

func TestInstallCodexMergesManagedBlock(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	cfg := filepath.Join(home, ".codex", "config.toml")
	if err := os.WriteFile(cfg, []byte("[profiles.default]\nmodel = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Install(Options{Target: TargetCodex, Features: []string{FeatureMCP}, HomeDir: home, WorkDir: work}); err != nil {
		t.Fatalf("Install codex: %v", err)
	}
	body, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "[profiles.default]") || !strings.Contains(text, "[mcp_servers.gum]") || !strings.Contains(text, `"--stdio"`) {
		t.Fatalf("merged TOML = %q", text)
	}
	if _, err := Install(Options{Target: TargetCodex, Features: []string{FeatureMCP}, HomeDir: home, WorkDir: work, MCPArgs: []string{"--log-level", "debug"}}); err != nil {
		t.Fatalf("Install codex replace: %v", err)
	}
	body, _ = os.ReadFile(cfg)
	if strings.Count(string(body), "[mcp_servers.gum]") != 1 || !strings.Contains(string(body), `"--log-level"`) {
		t.Fatalf("managed block not replaced cleanly: %q", string(body))
	}
}

func TestInstallRejectsUnmanagedCodexBlock(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatalf("mkdir .codex: %v", err)
	}
	cfg := filepath.Join(home, ".codex", "config.toml")
	if err := os.WriteFile(cfg, []byte("[mcp_servers.gum]\ncommand = \"gum\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Install(Options{Target: TargetCodex, Features: []string{FeatureMCP}, HomeDir: home, WorkDir: work})
	if err == nil || !strings.Contains(err.Error(), "outside the managed block") {
		t.Fatalf("err=%v", err)
	}
}

func TestMergeMCPJSONPreservesExistingServersAndRejectsBadJSON(t *testing.T) {
	raw, err := mergeMCPJSON([]byte(`{"mcpServers":{"other":{"command":"other"}}}`), DefaultToolset, []string{"--log-level", "debug"})
	if err != nil {
		t.Fatalf("mergeMCPJSON: %v", err)
	}
	if !strings.Contains(string(raw), `"other"`) || !strings.Contains(string(raw), `"gum"`) || !strings.Contains(string(raw), `"--log-level"`) {
		t.Fatalf("merged json=%s", raw)
	}
	if _, err := mergeMCPJSON([]byte(`{`), DefaultToolset, nil); err == nil {
		t.Fatal("bad json err=nil")
	}
}

func TestPathBranchHelpers(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	if got := skillRootFor(TargetClaude, ScopeUser, home, work); got != filepath.Join(home, ".claude", "skills") {
		t.Fatalf("claude user skill root=%q", got)
	}
	if got := skillRootFor(TargetClaude, ScopeProject, home, work); got != filepath.Join(work, ".claude", "skills") {
		t.Fatalf("claude project skill root=%q", got)
	}
	for _, target := range []string{TargetCodex, TargetClaude, TargetCursor, TargetGemini} {
		write := mcpWriteFor(target, ScopeProject, home, work, DefaultToolset, nil)
		if write.path == "" || write.merge == nil {
			t.Fatalf("mcpWriteFor(%s)=%#v", target, write)
		}
	}
	if _, err := installableRelativePath(".bad", "SKILL.md"); err == nil {
		t.Fatal("bad installable dir err=nil")
	}
	if _, err := installableRelativePath("gum", "../SKILL.md"); err == nil {
		t.Fatal("bad installable file err=nil")
	}
}

func TestResolveSafeWriteRejectsDirectoryAndParentFile(t *testing.T) {
	dir := t.TempDir()
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = realDir
	}
	if _, err := resolveSafeWrite(dir); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("directory err=%v", err)
	}
	base := t.TempDir()
	if realBase, err := filepath.EvalSymlinks(base); err == nil {
		base = realBase
	}
	parentFile := filepath.Join(base, "file")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	if _, err := resolveSafeWrite(filepath.Join(parentFile, "child")); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("parent file err=%v", err)
	}
}

func TestInstallSkipsExistingSkillUnlessForced(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	skillPath := filepath.Join(home, ".agents", "skills", "gum", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	canonicalSkillPath, err := filepath.EvalSymlinks(skillPath)
	if err == nil {
		skillPath = canonicalSkillPath
	}
	plan, err := Install(Options{Target: TargetCodex, Features: []string{FeatureSkills}, HomeDir: home, WorkDir: work})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	foundSkipped := false
	for _, action := range plan.Actions {
		if action.Path == skillPath && action.Status == "skipped" {
			foundSkipped = true
		}
	}
	if !foundSkipped {
		t.Fatalf("plan did not skip existing skill: %#v", plan.Actions)
	}
	body, _ := os.ReadFile(skillPath)
	if string(body) != "keep" {
		t.Fatalf("skill overwritten without force: %q", string(body))
	}
}
