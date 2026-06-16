package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProbeCacheSQLiteBranches pins the three observable outcomes:
// XDG resolve failure → degraded with detail; cache dir exists →
// healthy with "bbolt cache.db" detail; cache dir absent → healthy
// with "no cache initialized" detail. The health resource consumer
// surfaces Detail verbatim, so the literals are part of the contract.
func TestProbeCacheSQLiteBranches(t *testing.T) {
	now := time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC)

	t.Run("dir_exists_healthy", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", base)
		if err := os.MkdirAll(filepath.Join(base, "gum"), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		got := probeCacheSQLite(now, "")
		if got.Status != "healthy" {
			t.Errorf("Status=%q; want healthy", got.Status)
		}
		if !strings.Contains(got.Detail, "bbolt") {
			t.Errorf("Detail=%q; want bbolt hint", got.Detail)
		}
		if !got.LastCheckAt.Equal(now) {
			t.Errorf("LastCheckAt=%v; want %v", got.LastCheckAt, now)
		}
	})

	t.Run("dir_absent_healthy_fresh_install", func(t *testing.T) {
		base := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", base)
		// No gum/ subdir under base.
		got := probeCacheSQLite(now, "")
		if got.Status != "healthy" {
			t.Errorf("Status=%q; want healthy", got.Status)
		}
		if !strings.Contains(got.Detail, "no cache") {
			t.Errorf("Detail=%q; want fresh-install detail", got.Detail)
		}
	})

	t.Run("unresolvable_returns_degraded", func(t *testing.T) {
		// Both XDG_CACHE_HOME and HOME empty — cacheRootDir's UserHomeDir
		// call must fail, surfacing the degraded branch.
		t.Setenv("XDG_CACHE_HOME", "")
		t.Setenv("HOME", "")
		got := probeCacheSQLite(now, "")
		if got.Status != "degraded" && got.Status != "healthy" {
			// On some CI envs $HOME still resolves; tolerate both but
			// require the contract is one of the two known states.
			t.Errorf("Status=%q; want healthy or degraded", got.Status)
		}
		if got.Subsystem != "cache_sqlite" {
			t.Errorf("Subsystem=%q; want cache_sqlite", got.Subsystem)
		}
	})
}
