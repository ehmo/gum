package initpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestApplyNoOpLeavesTheFileUntouched pins Apply's `plan.NoOp → return nil`
// arm. The settings file below already carries the exact entry, written on a
// single line; Apply must leave those bytes alone rather than rewrite them in
// the canonical pretty-printed form.
func TestApplyNoOpLeavesTheFileUntouched(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := SettingsTarget{
		Path:     filepath.Join(dir, ".claude", "settings.json"),
		LockPath: filepath.Join(dir, ".claude", "settings.lock"),
	}
	if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := []byte(`{"mcpServers":{"gum":{"args":["mcp","--stdio"],"command":"gum"}}}`)
	if err := os.WriteFile(target.Path, body, 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	if err := Apply(target, "gum", DefaultMCPEntry(), time.Second); err != nil {
		t.Errorf("Apply(no-op) err=%v; want nil", err)
	}

	got, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("Apply rewrote an already-patched file:\n got %s\nwant %s", got, body)
	}
}

// TestApplyMkdirAllErrorWraps pins Apply's `os.MkdirAll err → wrap` arm.
// Reached by planting a regular file at the parent-dir chain so MkdirAll
// fails with ENOTDIR.
func TestApplyMkdirAllErrorWraps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Plant a regular file at dir/.claude so MkdirAll(dir/.claude/sub)
	// fails with ENOTDIR.
	blocker := filepath.Join(dir, ".claude")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	target := SettingsTarget{
		Path:     filepath.Join(blocker, "sub", "settings.json"),
		LockPath: filepath.Join(blocker, "sub", "settings.lock"),
	}
	err := Apply(target, "gum", DefaultMCPEntry(), time.Second)
	if err == nil {
		t.Fatal("Apply(blocked MkdirAll) err=nil; want mkdir wrap")
	}
	if !strings.Contains(err.Error(), "initpkg: mkdir") {
		t.Errorf("err=%v; want 'initpkg: mkdir' wrap", err)
	}
}
