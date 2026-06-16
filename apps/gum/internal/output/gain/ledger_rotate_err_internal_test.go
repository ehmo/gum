package gain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRotateLockedRenameFailureWraps pins rotateLocked's
// `os.Rename err → "rename: %w"` arm (ledger.go:411-413). When the
// source file has been deleted out from under the ledger (admin
// cleanup, log-rotate interference, container ephemeral fs blip),
// os.Rename returns ENOENT; the wrap MUST carry the "rename:" prefix
// so log readers can distinguish rename failures from open-new-segment
// failures (line 415-417's "open new:" wrap).
//
// We call rotateLocked directly under l.mu (the contract documented
// at ledger.go:404) after unlinking the source file. The open file
// descriptor keeps the inode alive, so Close + Rename are the only
// failure points; ENOENT lands on Rename.
func TestRotateLockedRenameFailureWraps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")
	l, err := NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	// Unlink the dir entry. l.file still references the inode so the
	// in-flight close doesn't cascade; os.Rename(l.path, ...) sees
	// "no such file or directory" and returns ENOENT.
	if err := os.Remove(path); err != nil {
		t.Fatalf("os.Remove: %v", err)
	}

	l.mu.Lock()
	rotErr := l.rotateLocked()
	l.mu.Unlock()
	if rotErr == nil {
		t.Fatal("rotateLocked() = nil; want ENOENT-wrapped error")
	}
	if !strings.Contains(rotErr.Error(), "rename:") {
		t.Errorf("rotateLocked() err=%q; want 'rename:' prefix", rotErr.Error())
	}
}

// TestAppendRotateFailureWarnsButReturnsNil pins Append's
// `rotErr := l.rotateLocked(); rotErr != nil → slog.Warn` arm
// (ledger.go:394-396). Append MUST NOT propagate a rotation failure
// as an error; the ledger remains writable on the current segment,
// and the spec contract says size-based rotation is best-effort
// (gum-ledger §12.3).
//
// We shrink maxLedgerSize so Append #1 itself triggers rotation
// (which succeeds), then unlink the active segment so Append #2's
// rotation attempt fails (Rename ENOENT on the vanished l.path).
// The post-Append #2 assertion is simply Append-returned-nil; the
// directory state after a successful Append #1 already contains a
// rotated file, so listings are not an interesting signal here.
func TestAppendRotateFailureWarnsButReturnsNil(t *testing.T) {
	prev := maxLedgerSize
	t.Cleanup(func() { maxLedgerSize = prev })
	maxLedgerSize = 1

	dir := t.TempDir()
	path := filepath.Join(dir, "gain-ledger.jsonl")
	l, err := NewLedger(path)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	variant := "v1"
	profile := "p1"
	e := Entry{
		Session: "cccccccc", OpID: "x.y.z",
		VariantID: &variant, OutputProfile: &profile,
		ArgsHash: "h", AuthSubjectFingerprint: "f",
		RawTokens: 10, ShapedTokens: 1,
		CacheStatus: "miss", FieldMaskStatus: "applied",
		OpFamily: "x.y", BaselineMethod: "fixture_replay",
	}
	// First Append rotates (size 60 header + 200 entry > 1) and opens
	// a fresh active segment.
	if err := l.Append(e); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	// Unlink the now-active segment so Append #2's internal rotation
	// hits ENOENT.
	if err := os.Remove(path); err != nil {
		t.Fatalf("os.Remove: %v", err)
	}
	// Append #2: Write succeeds (handle still alive), Stat reports
	// post-write size > threshold, rotateLocked fires, Rename hits
	// ENOENT → slog.Warn → Append returns nil.
	if err := l.Append(e); err != nil {
		t.Fatalf("Append (post-unlink) = %v; want nil (rotation failure must be logged, not returned)", err)
	}
}
