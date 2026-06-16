package auditlog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/auditlog"
)

// TestSyncAppendHardCeilingRotateErrorWraps pins syncAppend's
// `hard-ceiling rotate err → handleFailure("hard-ceiling rotate: %w")`
// arm (writer.go:437-440). When the 10 GB hard-ceiling triggers a
// rotation and that rotation fails (here: an un-removable planted
// archive prevents enforceMaxFilesLocked from pruning), the writer
// MUST surface the failure with the "hard-ceiling rotate:" prefix
// in audit.broken so operators can distinguish a normal threshold
// rotation breakdown from the spec §11 hard-ceiling escalation.
func TestSyncAppendHardCeilingRotateErrorWraps(t *testing.T) {
	dir := t.TempDir()

	// Plant an un-removable archive that will be the alphabetically-
	// first target of pruning during enforceMaxFilesLocked.
	blocker := filepath.Join(dir, "audit.20240101T000000Z.jsonl")
	if err := os.Mkdir(blocker, 0o700); err != nil {
		t.Fatalf("plant blocker dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocker, "stuck"), []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker contents: %v", err)
	}

	// hardCeiling=1 byte → every Append exceeds it → forces the
	// hard-ceiling arm rather than the normal size/age arm.
	// maxFiles=1 → after rotation the 2 archives (blocker +
	// freshly-rotated audit.<ts>.jsonl) exceed the cap by 1; the
	// alphabetically-first archive (the planted blocker dir) is the
	// Remove target → ENOTEMPTY → rotateLockedAtomic returns err.
	w, err := auditlog.New(dir,
		auditlog.WithMaxSizeBytes(0),
		auditlog.WithRetentionDays(0),
		auditlog.WithMaxFiles(1),
		auditlog.WithHardCeilingBytes(1),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First Append: audit.jsonl doesn't exist, so rotateLockedAtomic's
	// step-1 ErrNotExist branch creates the empty file and returns
	// nil without invoking enforceMaxFilesLocked. The append then
	// writes the entry to disk.
	w.Append(makeEntry("first-seed"))
	// Second Append: audit.jsonl exists with size>0, the hard-ceiling
	// arm fires, rotateLockedAtomic renames + recreates + calls
	// enforceMaxFilesLocked which trips on the un-removable archive.
	w.Append(makeEntry("triggers-hard-ceiling"))

	sentinel := filepath.Join(dir, "audit.broken")
	body, statErr := os.ReadFile(sentinel)
	if statErr != nil {
		t.Fatalf("audit.broken absent: %v", statErr)
	}
	if !strings.Contains(string(body), "hard-ceiling rotate:") {
		t.Errorf("audit.broken=%q; want 'hard-ceiling rotate:' prefix", body)
	}
}
