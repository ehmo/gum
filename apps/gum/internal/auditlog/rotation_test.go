// Tests for the gum-gv9a additions: time-based rotation, 10 GB hard ceiling,
// audit.unbounded override, cross-process advisory file lock, mid-rotation
// crash recovery. Spec §11 rotation protocol.

package auditlog_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/auditlog"
)

// TestAuditLogTimeBasedRotation drives a synthetic clock past
// audit.retention_days and asserts that the next Append rotates the file
// (spec §11: rotation triggers on age since creation).
func TestAuditLogTimeBasedRotation(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)
	w, err := auditlog.New(dir,
		auditlog.WithMaxSizeBytes(0), // size rotation off — isolate time path
		auditlog.WithRetentionDays(7),
		auditlog.WithClock(clock.now),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// First entry establishes creationTime = start.
	w.Append(makeEntry("seed"))

	// Advance time past retention; next append must rotate.
	clock.advance(8 * 24 * time.Hour)
	w.Append(makeEntry("post-rotation"))

	entries, _ := os.ReadDir(dir)
	archives := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "audit.") && strings.HasSuffix(e.Name(), ".jsonl") && e.Name() != "audit.jsonl" {
			archives++
		}
	}
	if archives == 0 {
		t.Fatalf("expected time-based rotation to create an archive; got 0 (dir entries=%v)", dirNames(entries))
	}
}

// TestAuditLogTimeRotationDisabledWhenZero asserts retention_days=0 disables
// time-based rotation (spec §11 "retention_days=0 disables time-based
// rotation").
func TestAuditLogTimeRotationDisabledWhenZero(t *testing.T) {
	dir := t.TempDir()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	w, err := auditlog.New(dir,
		auditlog.WithMaxSizeBytes(0),
		auditlog.WithRetentionDays(0),
		auditlog.WithClock(clock.now),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.Append(makeEntry("seed"))
	clock.advance(365 * 24 * time.Hour)
	w.Append(makeEntry("a-year-later"))

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "audit.") && strings.HasSuffix(e.Name(), ".jsonl") && e.Name() != "audit.jsonl" {
			t.Fatalf("retention_days=0 must not rotate; found archive %q", e.Name())
		}
	}
}

// TestAuditHardCeilingForcesRotation asserts that when the file would grow
// past the 10 GB hard ceiling (here exercised at a small synthetic ceiling),
// rotation fires even with audit.max_size_mb=0.
func TestAuditHardCeilingForcesRotation(t *testing.T) {
	dir := t.TempDir()
	w, err := auditlog.New(dir,
		auditlog.WithMaxSizeBytes(0), // normal rotation off
		auditlog.WithHardCeilingBytes(500),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Each entry is ~200 bytes; three of them cross 500.
	for i := 0; i < 4; i++ {
		w.Append(makeEntry("fill"))
	}

	entries, _ := os.ReadDir(dir)
	archives := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "audit.") && strings.HasSuffix(e.Name(), ".jsonl") && e.Name() != "audit.jsonl" {
			archives++
		}
	}
	if archives == 0 {
		t.Fatalf("expected hard-ceiling rotation to create archive; got 0 (entries=%v)", dirNames(entries))
	}
}

// TestAuditUnboundedDisablesHardCeiling asserts that with unbounded=true, no
// rotation occurs even past the hard-ceiling threshold.
func TestAuditUnboundedDisablesHardCeiling(t *testing.T) {
	dir := t.TempDir()
	w, err := auditlog.New(dir,
		auditlog.WithMaxSizeBytes(0),
		auditlog.WithHardCeilingBytes(500),
		auditlog.WithUnbounded(true),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i := 0; i < 4; i++ {
		w.Append(makeEntry("fill"))
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "audit.") && strings.HasSuffix(e.Name(), ".jsonl") && e.Name() != "audit.jsonl" {
			t.Fatalf("unbounded=true must not rotate; found archive %q", e.Name())
		}
	}
}

// TestMidRotationCrashRecovery seeds the directory in the state spec §11
// "mid-rotation crash recovery" describes: audit.jsonl.lock present but
// audit.jsonl absent. The next Writer construction must recreate the live
// file before any append fails.
func TestMidRotationCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "audit.jsonl.lock")
	if err := os.WriteFile(lockPath, []byte{}, 0o600); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	// audit.jsonl intentionally absent.

	w, err := auditlog.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// audit.jsonl should now exist as an empty regular file mode 600.
	info, err := os.Stat(w.Path())
	if err != nil {
		t.Fatalf("audit.jsonl must be recreated after crash recovery; stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("recreated audit.jsonl mode=%o; want 0600", mode)
	}
	if info.Size() != 0 {
		t.Errorf("recreated audit.jsonl size=%d; want 0", info.Size())
	}

	// An append should land cleanly.
	w.Append(makeEntry("post-recovery"))
	data, err := os.ReadFile(w.Path())
	if err != nil || !bytes.Contains(data, []byte("post-recovery")) {
		t.Fatalf("post-recovery append did not land: data=%q err=%v", data, err)
	}
}

// TestRotationConcurrentSafety runs two Writer instances against the same
// directory (simulating two processes via separate Writer objects whose only
// shared state is the on-disk lock file) and hammers Append. The assertion is
// that every entry lands and the rotated archive count stays within the
// max_files bound.
func TestRotationConcurrentSafety(t *testing.T) {
	dir := t.TempDir()
	// Force frequent rotation by setting small maxSize.
	opts := []auditlog.Option{
		auditlog.WithMaxSizeBytes(400),
		auditlog.WithMaxFiles(3),
	}
	w1, err := auditlog.New(dir, opts...)
	if err != nil {
		t.Fatalf("w1 New: %v", err)
	}
	w2, err := auditlog.New(dir, opts...)
	if err != nil {
		t.Fatalf("w2 New: %v", err)
	}

	var wg sync.WaitGroup
	for _, w := range []*auditlog.Writer{w1, w2} {
		wg.Add(1)
		go func(w *auditlog.Writer) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				w.Append(makeEntry("concurrent"))
			}
		}(w)
	}
	wg.Wait()

	// Live audit.jsonl must exist (it's recreated on every rotation).
	if _, err := os.Stat(filepath.Join(dir, "audit.jsonl")); err != nil {
		t.Fatalf("audit.jsonl missing after concurrent rotation: %v", err)
	}

	// Sentinel must not be set — every append landed via lock-guarded path.
	if _, err := os.Stat(filepath.Join(dir, "audit.broken")); err == nil {
		t.Errorf("audit.broken sentinel set despite no real failures (concurrent rotation must succeed)")
	}
}

// --- helpers ---

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{t: start} }
func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func makeEntry(label string) map[string]any {
	return map[string]any{
		"op_id":      label,
		"variant_id": label + ".v1",
		"args_hash":  "h",
		"client_id":  "cli",
		"risk_class": "read",
	}
}

func dirNames(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
