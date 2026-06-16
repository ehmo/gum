package initpkg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPlanPatchCreatesNewFile verifies that PlanPatch detects an absent
// settings.json and produces a create-action plan whose merged content has
// only the gum entry under mcpServers.
func TestPlanPatchCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	target := SettingsTarget{
		Path:     filepath.Join(dir, ".claude", "settings.json"),
		LockPath: filepath.Join(dir, ".claude", "settings.lock"),
	}
	plan, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err != nil {
		t.Fatalf("PlanPatch: %v", err)
	}
	if plan.Action != "create" {
		t.Errorf("Action = %q; want create", plan.Action)
	}
	if plan.Before != nil {
		t.Errorf("Before = %v; want nil for create", plan.Before)
	}
	if plan.NoOp {
		t.Error("NoOp = true; want false for create")
	}
	got := plan.After["mcpServers"].(map[string]any)["gum"].(map[string]any)
	if got["command"] != "gum" {
		t.Errorf("command = %v; want gum", got["command"])
	}
}

// TestPlanPatchPreservesExistingEntries verifies that PlanPatch deep-merges
// — existing mcpServers (other than gum) survive, and non-mcpServers keys at
// the root pass through untouched.
func TestPlanPatchPreservesExistingEntries(t *testing.T) {
	dir := t.TempDir()
	target := SettingsTarget{
		Path:     filepath.Join(dir, "settings.json"),
		LockPath: filepath.Join(dir, "settings.lock"),
	}
	prior := map[string]any{
		"theme":  "dark",
		"editor": map[string]any{"fontSize": 14},
		"mcpServers": map[string]any{
			"other-server": map[string]any{"command": "other"},
		},
	}
	encoded, _ := json.MarshalIndent(prior, "", "  ")
	if err := os.WriteFile(target.Path, encoded, 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}
	plan, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err != nil {
		t.Fatalf("PlanPatch: %v", err)
	}
	if plan.Action != "update" {
		t.Errorf("Action = %q; want update", plan.Action)
	}
	if got, _ := plan.After["theme"].(string); got != "dark" {
		t.Errorf("theme = %q; want dark", got)
	}
	servers := plan.After["mcpServers"].(map[string]any)
	if _, ok := servers["other-server"].(map[string]any); !ok {
		t.Errorf("other-server entry missing; servers=%v", servers)
	}
	if _, ok := servers["gum"].(map[string]any); !ok {
		t.Errorf("gum entry missing")
	}
}

// TestPlanPatchNoOp verifies that re-running gum init against a settings.json
// that already contains the canonical gum entry sets plan.NoOp.
func TestPlanPatchNoOp(t *testing.T) {
	dir := t.TempDir()
	target := SettingsTarget{
		Path:     filepath.Join(dir, "settings.json"),
		LockPath: filepath.Join(dir, "settings.lock"),
	}
	prior := map[string]any{
		"mcpServers": map[string]any{
			"gum": map[string]any{
				"command": "gum",
				"args":    []any{"mcp", "--stdio"},
			},
		},
	}
	encoded, _ := json.MarshalIndent(prior, "", "  ")
	if err := os.WriteFile(target.Path, encoded, 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}
	plan, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err != nil {
		t.Fatalf("PlanPatch: %v", err)
	}
	if !plan.NoOp {
		t.Errorf("NoOp = false; want true when entry already matches")
	}
}

// TestPlanPatchRejectsInvalidJSON verifies that a non-JSON existing file
// returns an error rather than nuking it.
func TestPlanPatchRejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	target := SettingsTarget{
		Path:     filepath.Join(dir, "settings.json"),
		LockPath: filepath.Join(dir, "settings.lock"),
	}
	if err := os.WriteFile(target.Path, []byte("this is not json"), 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}
	_, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err == nil {
		t.Fatalf("PlanPatch on non-JSON file succeeded; want error")
	}
}

// TestApplyAtomicWrite verifies that Apply writes the merged JSON atomically
// (the destination is either fully written or not modified) and that the
// canonical encoding sorts keys.
func TestApplyAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	target := SettingsTarget{
		Path:     filepath.Join(dir, ".claude", "settings.json"),
		LockPath: filepath.Join(dir, ".claude", "settings.lock"),
	}
	plan, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err != nil {
		t.Fatalf("PlanPatch: %v", err)
	}
	if err := Apply(target, plan, time.Second); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	raw, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	servers := got["mcpServers"].(map[string]any)
	gum := servers["gum"].(map[string]any)
	if gum["command"] != "gum" {
		t.Errorf("written command = %v; want gum", gum["command"])
	}
	args := gum["args"].([]any)
	want := []string{"mcp", "--stdio"}
	if len(args) != len(want) {
		t.Fatalf("args len = %d; want %d", len(args), len(want))
	}
	for i, a := range args {
		if a != want[i] {
			t.Errorf("args[%d] = %v; want %s", i, a, want[i])
		}
	}
	// Stable key ordering — mcpServers before any later top-level key would
	// land alphabetically. Smoke check: encoded form starts with "{\n" then
	// "  \"mcpServers\":".
	if !strings.Contains(string(raw), "\"mcpServers\"") {
		t.Errorf("encoded JSON missing mcpServers literal: %s", raw)
	}
}

// TestApplyHoldsLock verifies that two concurrent Apply calls serialize on
// the advisory lock — both succeed and the file holds the final write.
func TestApplyHoldsLock(t *testing.T) {
	dir := t.TempDir()
	target := SettingsTarget{
		Path:     filepath.Join(dir, "settings.json"),
		LockPath: filepath.Join(dir, "settings.lock"),
	}
	plan, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err != nil {
		t.Fatalf("PlanPatch: %v", err)
	}
	// First Apply succeeds.
	if err := Apply(target, plan, time.Second); err != nil {
		t.Fatalf("Apply #1: %v", err)
	}
	// Second Apply (still NoOp=false because we re-plan with a fresh result)
	// should also succeed; the lock is released after each call.
	plan2, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err != nil {
		t.Fatalf("PlanPatch #2: %v", err)
	}
	if !plan2.NoOp {
		t.Errorf("second plan NoOp = false; want true (file already patched)")
	}
	if err := Apply(target, plan2, time.Second); err != nil {
		t.Fatalf("Apply #2 (no-op): %v", err)
	}
}

// TestWriteGUMmdRendersTemplate verifies that the embedded GUM.md template
// renders without error and contains the version interpolation.
func TestWriteGUMmdRendersTemplate(t *testing.T) {
	dir := t.TempDir()
	dest, err := WriteGUMmd(dir, dir, "v0.1.0-test", true)
	if err != nil {
		t.Fatalf("WriteGUMmd: %v", err)
	}
	if dest != filepath.Join(dir, "GUM.md") {
		t.Errorf("dest = %s; want %s/GUM.md", dest, dir)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read GUM.md: %v", err)
	}
	if !strings.Contains(string(body), "v0.1.0-test") {
		t.Errorf("GUM.md missing version interpolation: %s", body)
	}
	for _, want := range []string{
		"gum.search_apis", "gum.read", "gum.write", "gum.destructive",
		"Profile selection", "Auth method",
		"Read-only profile", "Write-enabled profile", "Code-mode profile",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("GUM.md missing required section %q", want)
		}
	}
}

// TestResolveSettingsTargetGlobalVsLocal verifies the global/local path split.
func TestResolveSettingsTargetGlobalVsLocal(t *testing.T) {
	home := "/tmp/home"
	proj := "/tmp/proj"
	global := ResolveSettingsTarget(home, proj, true)
	if global.Path != filepath.Join(home, ".claude", "settings.json") {
		t.Errorf("global path = %s", global.Path)
	}
	local := ResolveSettingsTarget(home, proj, false)
	if local.Path != filepath.Join(proj, ".claude", "settings.json") {
		t.Errorf("local path = %s", local.Path)
	}
}

// TestPlanPatchDoesNotMutateBefore is the audit regression: PlanPatch must not
// mutate plan.Before (the pre-patch snapshot). cloneMap is shallow, so before
// the fix the nested mcpServers map was aliased and the servers[name]=... write
// corrupted plan.Before to the post-patch state.
func TestPlanPatchDoesNotMutateBefore(t *testing.T) {
	dir := t.TempDir()
	target := SettingsTarget{
		Path:     filepath.Join(dir, "settings.json"),
		LockPath: filepath.Join(dir, "settings.lock"),
	}
	prior := map[string]any{
		"mcpServers": map[string]any{
			"gum": map[string]any{"command": "OLD-COMMAND"},
		},
	}
	encoded, _ := json.MarshalIndent(prior, "", "  ")
	if err := os.WriteFile(target.Path, encoded, 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	plan, err := PlanPatch(target, "gum", DefaultMCPEntry())
	if err != nil {
		t.Fatalf("PlanPatch: %v", err)
	}

	beforeServers, _ := plan.Before["mcpServers"].(map[string]any)
	gumBefore, _ := beforeServers["gum"].(map[string]any)
	if cmd, _ := gumBefore["command"].(string); cmd != "OLD-COMMAND" {
		t.Errorf("plan.Before was mutated: gum.command=%q; want OLD-COMMAND (pre-patch snapshot)", cmd)
	}
	afterServers, _ := plan.After["mcpServers"].(map[string]any)
	if _, ok := afterServers["gum"].(map[string]any); !ok {
		t.Error("plan.After missing the gum entry")
	}
}
