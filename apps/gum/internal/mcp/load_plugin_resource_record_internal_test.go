package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadPluginResourceRecordEmptyShortCircuit pins the (profileDir==""
// || name=="") guard: both inputs must be non-empty before any disk
// access happens, otherwise the resources/read handler would surface
// confusing "no row" errors for malformed reads.
func TestLoadPluginResourceRecordEmptyShortCircuit(t *testing.T) {
	t.Run("blank_name_returns_false", func(t *testing.T) {
		// Set up an env that yields a non-empty profileDir.
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		s := &Server{profile: "default"}
		rec, ok := s.loadPluginResourceRecord("")
		if ok || rec != nil {
			t.Errorf("blank name: ok=%v rec=%v; want (nil, false)", ok, rec)
		}
	})

	t.Run("empty_profile_dir_returns_false", func(t *testing.T) {
		// Both XDG_DATA_HOME and HOME blanked → profilePluginDir() returns "".
		t.Setenv("XDG_DATA_HOME", "")
		t.Setenv("HOME", "")
		s := &Server{}
		rec, ok := s.loadPluginResourceRecord("anything")
		if ok || rec != nil {
			t.Errorf("empty profile dir: ok=%v rec=%v; want (nil, false)", ok, rec)
		}
	})
}

// TestLoadPluginResourceRecordNoLockNoState pins the second guard:
// when neither plugins.lock nor plugin-state.json carries a matching
// row, the function must return (nil, false) so resources/read returns
// 404, not an empty record.
func TestLoadPluginResourceRecordNoLockNoState(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	// Set up an empty profile dir (no files at all).
	if err := os.MkdirAll(filepath.Join(dataHome, "gum", "default"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Server{profile: "default"}
	rec, ok := s.loadPluginResourceRecord("unknown.plugin")
	if ok || rec != nil {
		t.Errorf("missing rows: ok=%v rec=%v; want (nil, false)", ok, rec)
	}
}

// TestLoadPluginResourceRecordInstalledPendingRestart pins the
// "installed_pending_restart" status branch, including the
// reason-fallback to "restart_required" when state.reason is blank.
func TestLoadPluginResourceRecordInstalledPendingRestart(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Lock row supplies version/description; state row supplies
	// installed_pending_restart status without a reason so we hit the
	// "restart_required" fallback.
	lock := `{"plugins":[{"name":"pending.example","version":"1.0.0"}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugins.lock"), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	state := `{"plugins":[{"name":"pending.example","status":"installed_pending_restart","activated_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}

	s := &Server{profile: "default"}
	rec, ok := s.loadPluginResourceRecord("pending.example")
	if !ok || rec == nil {
		t.Fatalf("ok=%v rec=%v; want valid record", ok, rec)
	}
	if rec.Status != "installed_pending_restart" {
		t.Errorf("status=%q; want installed_pending_restart", rec.Status)
	}
	if rec.Reason != "restart_required" {
		t.Errorf("reason=%q; want fallback 'restart_required'", rec.Reason)
	}
	if rec.ActivatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("activated_at=%q", rec.ActivatedAt)
	}
	// VariantIDs must be non-nil (empty slice, not nil) so the JSON
	// surface emits [] rather than null.
	if rec.VariantIDs == nil {
		t.Errorf("VariantIDs is nil; want empty slice")
	}
}
