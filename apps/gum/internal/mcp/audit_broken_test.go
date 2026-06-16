// audit_broken sentinel tests for handleCacheStats (spec §11 §2333-2336).
//
// audit_broken is true exactly when ~/.local/share/gum/<profile>/audit.broken
// exists on disk. Both tests redirect XDG_DATA_HOME to a tempdir to avoid
// leaking the developer's real audit state into the test result.
package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCacheStatsAuditBrokenTrueWhenSentinelPresent seeds the sentinel file
// under the active profile path and asserts audit_broken == true.
func TestCacheStatsAuditBrokenTrueWhenSentinelPresent(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	profileDir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "audit.broken"), []byte("2026-01-01T00:00:00Z disk full"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	srv := NewServer(noopDispatcher{})
	m := invokeCacheStats(t, srv)

	raw, ok := m["audit_broken"]
	if !ok {
		t.Fatal("audit_broken key missing")
	}
	ab, ok := raw.(bool)
	if !ok {
		t.Fatalf("audit_broken is %T; want bool", raw)
	}
	if !ab {
		t.Error("audit_broken = false; want true when sentinel exists")
	}
}

// TestCacheStatsAuditBrokenHonorsProfile asserts that the sentinel probe
// follows the SetProfile override rather than always using "default".
func TestCacheStatsAuditBrokenHonorsProfile(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	// Seed sentinel under profile "team-a" but NOT under "default".
	profileDir := filepath.Join(dataHome, "gum", "team-a")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("mkdir profile dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "audit.broken"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	srv := NewServer(noopDispatcher{})

	// Default profile → no sentinel → audit_broken false.
	m := invokeCacheStats(t, srv)
	if ab, _ := m["audit_broken"].(bool); ab {
		t.Error("default profile: audit_broken = true; want false (sentinel under team-a, not default)")
	}

	// Switch profile → sentinel found → audit_broken true.
	_ = srv.SetProfile("team-a")
	m = invokeCacheStats(t, srv)
	if ab, _ := m["audit_broken"].(bool); !ab {
		t.Error("team-a profile: audit_broken = false; want true (sentinel exists under team-a)")
	}
}
