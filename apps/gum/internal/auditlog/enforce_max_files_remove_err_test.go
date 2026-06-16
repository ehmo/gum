package auditlog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/auditlog"
)

// TestEnforceMaxFilesLockedRemoveErrorBubblesUp pins
// enforceMaxFilesLocked's `os.Remove err → return err` arm
// (writer.go:623-625). When the oldest archive cannot be removed
// (planted as a non-empty directory at that path, so os.Remove
// returns ENOTEMPTY), the rotation MUST fail loudly via the
// audit.broken sentinel rather than silently leak retention
// state — operators reading audit.broken see "rotate: ..."
// and know to clean up by hand.
//
// The planted archives sort alphabetically; archives[0] is the
// oldest. We plant audit.<oldest>.jsonl as a directory containing
// a file so os.Remove fails on the first delete; the rotation
// path surfaces this through handleFailure → audit.broken.
func TestEnforceMaxFilesLockedRemoveErrorBubblesUp(t *testing.T) {
	dir := t.TempDir()

	// Seed two normal archives + one un-removable archive (a directory
	// containing a file). Sorted alphabetically, the un-removable one
	// is the oldest and will be the first target of pruning.
	blocker := filepath.Join(dir, "audit.20240101T000000Z.jsonl")
	if err := os.Mkdir(blocker, 0o700); err != nil {
		t.Fatalf("plant blocker dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocker, "stuck"), []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker contents: %v", err)
	}
	for _, name := range []string{
		"audit.20240102T000000Z.jsonl",
		"audit.20240103T000000Z.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	// MaxFiles=2 means after the rotation creates a 4th archive, the
	// 2 oldest must be pruned. The very first prune target is the
	// planted directory → os.Remove ENOTEMPTY → err.
	w, err := auditlog.New(dir,
		auditlog.WithMaxSizeBytes(0),
		auditlog.WithRetentionDays(1),
		auditlog.WithMaxFiles(2),
		auditlog.WithClock(clock.now),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w.Append(makeEntry("seed"))
	clock.advance(48 * time.Hour)
	w.Append(makeEntry("after-rotate"))

	// audit.broken must exist with a "rotate:" prefix carrying the
	// underlying Remove error.
	sentinel := filepath.Join(dir, "audit.broken")
	body, statErr := os.ReadFile(sentinel)
	if statErr != nil {
		t.Fatalf("audit.broken absent after Remove-failed rotation: %v", statErr)
	}
	if !strings.Contains(string(body), "rotate:") {
		t.Errorf("audit.broken=%q; want 'rotate:' prefix", body)
	}

	// Blocker directory must still be present — Remove failed, so
	// nothing should have been deleted under it.
	if _, err := os.Stat(blocker); err != nil {
		t.Errorf("blocker dir gone after failed Remove: %v", err)
	}
}
