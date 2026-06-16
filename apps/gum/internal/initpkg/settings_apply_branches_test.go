package initpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestApplyNoOpReturnsNilImmediately pins Apply's `plan.NoOp →
// return nil` arm (settings.go:119-120). A no-op plan must short-
// circuit before MkdirAll/acquireSettingsLock so a read-only run
// (already-merged settings) doesn't touch the filesystem.
func TestApplyNoOpReturnsNilImmediately(t *testing.T) {
	t.Parallel()
	target := SettingsTarget{
		Path:     "/should/not/be/touched.json",
		LockPath: "/should/not/be/touched.lock",
	}
	plan := &PatchPlan{NoOp: true}
	if err := Apply(target, plan, time.Second); err != nil {
		t.Errorf("Apply(NoOp) err=%v; want nil short-circuit", err)
	}
}

// TestApplyMkdirAllErrorWraps pins Apply's `os.MkdirAll err → wrap`
// arm (settings.go:122-124). Reached by planting a regular file at
// the parent-dir chain so MkdirAll fails with ENOTDIR.
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
	plan := &PatchPlan{PatchedBytes: []byte("{}")}
	err := Apply(target, plan, time.Second)
	if err == nil {
		t.Fatal("Apply(blocked MkdirAll) err=nil; want mkdir wrap")
	}
	if !strings.Contains(err.Error(), "initpkg: mkdir") {
		t.Errorf("err=%v; want 'initpkg: mkdir' wrap", err)
	}
}
