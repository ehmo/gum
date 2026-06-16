package auditlog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/auditlog"
)

// TestEnforceMaxFilesLockedNegativeMaxFilesShortCircuits pins the
// `maxFiles < 0 → return nil` arm: a negative WithMaxFiles MUST disable
// retention pruning entirely (operator escape-hatch when they want to
// retain forever). Forced by triggering a time-based rotation, then
// asserting the rotation-created archive plus pre-seeded archives all
// survive — pruning would have removed the oldest.
func TestEnforceMaxFilesLockedNegativeMaxFilesShortCircuits(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed extra archives that would normally be pruned.
	for _, name := range []string{
		"audit.20240101T000000Z.jsonl",
		"audit.20240102T000000Z.jsonl",
		"audit.20240103T000000Z.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	w, err := auditlog.New(dir,
		auditlog.WithMaxSizeBytes(0),
		auditlog.WithRetentionDays(1),
		auditlog.WithMaxFiles(-1),
		auditlog.WithClock(clock.now),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Seed (creates audit.jsonl with creationTime=start).
	w.Append(makeEntry("seed"))
	// Advance past retention so the next Append triggers rotation
	// → enforceMaxFilesLocked is invoked, hits the negative-maxFiles
	// early return.
	clock.advance(48 * time.Hour)
	w.Append(makeEntry("after-rotate"))

	// All seeded archives MUST still exist despite rotation; pruning
	// was skipped because maxFiles<0.
	for _, name := range []string{
		"audit.20240101T000000Z.jsonl",
		"audit.20240102T000000Z.jsonl",
		"audit.20240103T000000Z.jsonl",
	} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Errorf("archive %s pruned despite maxFiles=-1: %v", name, statErr)
		}
	}

	// Sanity: rotation actually happened (new audit.<ts>.jsonl present).
	entries, _ := os.ReadDir(dir)
	rotated := false
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "audit.2026") && strings.HasSuffix(n, ".jsonl") {
			rotated = true
			break
		}
	}
	if !rotated {
		t.Fatal("rotation never fired; test premise broken — enforceMaxFilesLocked may not have run")
	}
}

// TestHandleFailureSentinelWriteErrorIsSwallowed pins the
// `WriteFile(sentinelPath) err → slog.Error + continue` arm of
// handleFailure: when the sentinel itself cannot be written (planted
// directory at the path), the writer MUST NOT panic — availability
// trumps observability per spec §11. Triggered by feeding Append a
// non-JSON-encodable map AFTER planting a directory where audit.broken
// would live.
func TestHandleFailureSentinelWriteErrorIsSwallowed(t *testing.T) {
	dir := t.TempDir()

	// Plant a directory at audit.broken so os.WriteFile fails EISDIR.
	if err := os.Mkdir(filepath.Join(dir, "audit.broken"), 0o700); err != nil {
		t.Fatalf("plant sentinel blocker: %v", err)
	}

	w, err := auditlog.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Force a marshal failure → handleFailure path → WriteFile sentinel
	// fails EISDIR. The append must NOT panic; the directory must NOT
	// be replaced.
	w.Append(map[string]any{"bad": make(chan int)})

	st, statErr := os.Stat(filepath.Join(dir, "audit.broken"))
	if statErr != nil {
		t.Fatalf("sentinel path gone after failure: %v", statErr)
	}
	if !st.IsDir() {
		t.Error("sentinel path is no longer a directory; WriteFile must have failed silently, leaving the planted dir intact")
	}
}
