package auditlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestInitCreationTimeShapes pins the three observable branches: no
// live file falls back to now(); a JSONL with a valid first ts wins
// over mtime; an empty/malformed file falls back to mtime via the
// info-only branch.
func TestInitCreationTimeShapes(t *testing.T) {
	dir := t.TempDir()
	fixedNow := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return fixedNow }

	t.Run("missing_file_falls_back_to_now", func(t *testing.T) {
		w := &Writer{path: filepath.Join(dir, "missing.jsonl"), now: nowFn}
		w.initCreationTime()
		if !w.creationTime.Equal(fixedNow) {
			t.Errorf("creationTime=%v; want fixedNow", w.creationTime)
		}
	})

	t.Run("empty_file_falls_back_to_now", func(t *testing.T) {
		p := filepath.Join(dir, "empty.jsonl")
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		w := &Writer{path: p, now: nowFn}
		w.initCreationTime()
		if !w.creationTime.Equal(fixedNow) {
			t.Errorf("creationTime=%v; want fixedNow", w.creationTime)
		}
	})

	t.Run("valid_first_ts_wins", func(t *testing.T) {
		ts := time.Date(2026, 1, 1, 6, 30, 0, 0, time.UTC)
		p := filepath.Join(dir, "real.jsonl")
		line := `{"ts":"` + ts.Format(time.RFC3339Nano) + `","tool":"x"}` + "\n"
		if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		w := &Writer{path: p, now: nowFn}
		w.initCreationTime()
		if !w.creationTime.Equal(ts) {
			t.Errorf("creationTime=%v; want %v (from first line)", w.creationTime, ts)
		}
	})

	t.Run("malformed_first_line_falls_back_to_mtime", func(t *testing.T) {
		p := filepath.Join(dir, "garbage.jsonl")
		if err := os.WriteFile(p, []byte("not json\n"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		mtime := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		w := &Writer{path: p, now: nowFn}
		w.initCreationTime()
		// On systems where the FS truncates the timestamp resolution,
		// compare in second precision.
		if w.creationTime.Unix() != mtime.Unix() {
			t.Errorf("creationTime=%v; want mtime=%v", w.creationTime, mtime)
		}
	})
}
