package initpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPlanPatchUnreadableSettingsWraps pins the non-ErrNotExist ReadFile arm
// (settings.go:85). A directory planted at settings.json reads as EISDIR, so
// the plan must fail instead of treating the target as absent.
func TestPlanPatchUnreadableSettingsWraps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("plant directory: %v", err)
	}
	target := SettingsTarget{Path: path, LockPath: filepath.Join(dir, "settings.lock")}

	_, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err == nil {
		t.Fatal("PlanPatch err=nil; want the read failure")
	}
	if !strings.Contains(err.Error(), "initpkg: read ") {
		t.Errorf("err=%v; want the read arm", err)
	}
}

// TestApplyLockFailureSurfaces pins the acquireSettingsLock arm
// (settings.go:134). A directory at the lock path cannot be opened O_RDWR,
// so Apply must refuse before it writes anything.
func TestApplyLockFailureSurfaces(t *testing.T) {
	dir := t.TempDir()
	target := SettingsTarget{
		Path:     filepath.Join(dir, "settings.json"),
		LockPath: filepath.Join(dir, "settings.lock"),
	}
	if err := os.Mkdir(target.LockPath, 0o755); err != nil {
		t.Fatalf("plant lock directory: %v", err)
	}
	plan, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err != nil {
		t.Fatalf("PlanPatch: %v", err)
	}

	err = Apply(target, plan, time.Second)
	if err == nil {
		t.Fatal("Apply err=nil; want the lock failure")
	}
	if !strings.Contains(err.Error(), "open settings lock") {
		t.Errorf("err=%v; want the lock-open arm", err)
	}
	if _, statErr := os.Stat(target.Path); statErr == nil {
		t.Error("Apply wrote settings.json after the lock failed")
	}
}

// TestCanonicalJSONEncodeFailure pins the encoder arm (settings.go:167). A
// channel has no JSON representation, so Encode reports the unsupported type.
func TestCanonicalJSONEncodeFailure(t *testing.T) {
	_, err := canonicalJSON(make(chan int))
	if err == nil {
		t.Fatal("canonicalJSON(chan) err=nil; want an unsupported-type error")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("err=%v; want the unsupported-type error", err)
	}
}
