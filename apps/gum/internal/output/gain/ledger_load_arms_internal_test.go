package gain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEnsureLoadedIgnoresADirectoryPath pins the IsDir guard. os.Open on a
// directory succeeds on unix, and the scan that follows would return EISDIR
// on the first read; the guard turns that into an empty ledger instead of a
// warning on every Stats call.
func TestEnsureLoadedIgnoresADirectoryPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l := &Ledger{path: dir}

	l.mu.Lock()
	l.ensureLoadedLocked()
	l.mu.Unlock()

	if len(l.entries) != 0 {
		t.Errorf("entries = %d; want 0 for a directory path", len(l.entries))
	}
}

// TestEnsureLoadedSurvivesAnOverlongLine pins the scanner.Err arm. A record
// past maxLedgerLineBytes stops the scan; the entries read before it must
// still count, and the failure must not reach the caller (Stats has no error
// return).
func TestEnsureLoadedSurvivesAnOverlongLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")

	good := `{"record_type":"entry","op_id":"a.b","raw_tokens":100,"shaped_tokens":40}`
	huge := `{"record_type":"entry","op_id":"` + strings.Repeat("x", maxLedgerLineBytes+1) + `"}`
	if err := os.WriteFile(path, []byte(good+"\n"+huge+"\n"), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}

	l := &Ledger{path: path}
	l.mu.Lock()
	l.ensureLoadedLocked()
	l.mu.Unlock()

	if len(l.entries) != 1 {
		t.Fatalf("entries = %d; want the one record read before the overlong line", len(l.entries))
	}
	if l.entries[0].OpID != "a.b" {
		t.Errorf("entry op_id = %q; want a.b", l.entries[0].OpID)
	}
}

// TestAppendTracksEntriesOnceTheReadSideIsLive pins Append's in-memory
// tracking arm. Once Stats has loaded the file, a later Append must join
// l.entries so the next Stats call sees it without re-reading the file.
func TestAppendTracksEntriesOnceTheReadSideIsLive(t *testing.T) {
	t.Parallel()
	l, err := NewLedger(filepath.Join(t.TempDir(), "gain-ledger.jsonl"))
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	// Stats marks the ledger loaded; before it, Append leaves l.entries alone
	// so the first read does not double-count.
	if got := l.Stats().TotalCalls; got != 0 {
		t.Fatalf("TotalCalls = %d; want 0 on a fresh ledger", got)
	}

	if err := l.Append(Entry{OpID: "a.b", RawTokens: 100, ShapedTokens: 40}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	st := l.Stats()
	if st.TotalCalls != 1 || st.TotalTokensSaved != 60 {
		t.Errorf("stats = %+v; want 1 call and 60 tokens saved", st)
	}
}

// TestStatsByOpSkipsEntriesOutsideTheWindow pins the window filter. An entry
// stamped before `since` must not reach the per-op aggregate.
func TestStatsByOpSkipsEntriesOutsideTheWindow(t *testing.T) {
	t.Parallel()
	l := &Ledger{
		loaded: true,
		entries: []Entry{
			{OpID: "a.b", Timestamp: "2020-01-01T00:00:00Z", RawTokens: 100, ShapedTokens: 40},
			{OpID: "c.d", Timestamp: "2030-01-01T00:00:00Z", RawTokens: 200, ShapedTokens: 50},
		},
	}

	since := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	got := l.StatsByOp(since, time.Time{})

	if _, ok := got["a.b"]; ok {
		t.Error("op a.b is outside the window and must not appear")
	}
	st, ok := got["c.d"]
	if !ok {
		t.Fatalf("op c.d missing; got %v", got)
	}
	if st.TotalTokensSaved != 150 {
		t.Errorf("c.d saved = %d; want 150", st.TotalTokensSaved)
	}
}

// TestRotateLockedSyncFailureWraps pins rotateLocked's fsync arm. The rename
// must not happen when the active segment could not be flushed, or the
// archived file loses its tail.
func TestRotateLockedSyncFailureWraps(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "gain-ledger.jsonl")
	l, err := NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	// Close the handle without clearing the field so Sync returns EBADF.
	if err := l.file.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}

	l.mu.Lock()
	rotErr := l.rotateLocked()
	l.mu.Unlock()

	if rotErr == nil || !strings.Contains(rotErr.Error(), "sync before rotate") {
		t.Fatalf("rotateLocked() = %v; want a 'sync before rotate' wrap", rotErr)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("stat active segment: %v; a failed sync must leave the file in place", statErr)
	}
}
