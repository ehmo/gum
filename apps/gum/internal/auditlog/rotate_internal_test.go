package auditlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestWriter builds a Writer rooted at a tempdir with a fixed clock.
// It mirrors the production New() call so rotateLockedAtomic runs against
// realistic state (the file lock, sentinel paths, etc. are all set).
func newTestWriter(t *testing.T, now func() time.Time) *Writer {
	t.Helper()
	w, err := New(t.TempDir(), WithClock(now))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return w
}

// TestRotateLockedAtomicPeerAlreadyRotated drives the early-out branch:
// when audit.jsonl already exists at size 0 (because a peer process beat
// us to the rotation), rotateLockedAtomic must NOT create an archive and
// must reset creationTime so the age threshold restarts cleanly.
func TestRotateLockedAtomicPeerAlreadyRotated(t *testing.T) {
	now := time.Now()
	w := newTestWriter(t, func() time.Time { return now })

	// Create audit.jsonl at zero bytes (simulate peer-rotated state).
	if err := os.WriteFile(w.path, nil, 0o600); err != nil {
		t.Fatalf("seed empty: %v", err)
	}

	// Advance the clock so we can detect that creationTime got reset.
	now = now.Add(time.Hour)
	if err := w.rotateLockedAtomic(""); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// No archive should have been produced.
	entries, _ := os.ReadDir(w.dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "audit.") && strings.HasSuffix(e.Name(), ".jsonl") && e.Name() != "audit.jsonl" {
			t.Errorf("unexpected archive %q after peer-rotated early-out", e.Name())
		}
	}
	if !w.creationTime.Equal(now) {
		t.Errorf("creationTime=%v; want %v (reset on early-out)", w.creationTime, now)
	}
}

// TestRotateLockedAtomicMissingFileRecreates drives the
// `errors.Is(err, os.ErrNotExist)` branch on the initial stat: when
// audit.jsonl is missing entirely (e.g. user deleted it), the rotator
// must recreate an empty file and reset creationTime without producing
// an archive.
func TestRotateLockedAtomicMissingFileRecreates(t *testing.T) {
	now := time.Now()
	w := newTestWriter(t, func() time.Time { return now })

	// New() doesn't pre-create audit.jsonl; the missing-file branch is
	// the default post-construction state, so no setup needed.
	now = now.Add(2 * time.Hour)
	if err := w.rotateLockedAtomic(""); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	fi, err := os.Stat(w.path)
	if err != nil {
		t.Fatalf("stat after recreate: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("recreated audit.jsonl size=%d; want 0", fi.Size())
	}
	if !w.creationTime.Equal(now) {
		t.Errorf("creationTime=%v; want %v", w.creationTime, now)
	}
}

// TestRotateLockedAtomicHappyPathArchives drives the full step-2/step-3
// path: a non-empty audit.jsonl is renamed to audit.<iso>.jsonl and a
// fresh empty live file replaces it.
func TestRotateLockedAtomicHappyPathArchives(t *testing.T) {
	now := time.Now().UTC()
	w := newTestWriter(t, func() time.Time { return now })

	// Write a non-zero payload so the early-out doesn't fire.
	if err := os.WriteFile(w.path, []byte(`{"a":1}`+"\n"), 0o600); err != nil {
		t.Fatalf("seed audit.jsonl: %v", err)
	}

	if err := w.rotateLockedAtomic(""); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// Fresh empty live file present.
	fi, err := os.Stat(w.path)
	if err != nil {
		t.Fatalf("stat live: %v", err)
	}
	if fi.Size() != 0 {
		t.Errorf("live audit.jsonl size=%d; want 0", fi.Size())
	}

	// Archive named audit.<iso>.jsonl present.
	entries, _ := os.ReadDir(w.dir)
	archives := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "audit.") && strings.HasSuffix(name, ".jsonl") && name != "audit.jsonl" {
			archives++
		}
	}
	if archives != 1 {
		t.Errorf("archive count=%d; want 1", archives)
	}
}

// TestRotateLockedAtomicArchiveCollisionAppendsCounter drives the inner
// `for i := 1; i < 1000` loop: when a same-second archive already exists
// at the canonical path, the rotator appends `.1`, `.2`, … to avoid
// clobbering. We seed two archives with deterministic timestamps and
// verify the third rotation lands at `.2`.
func TestRotateLockedAtomicArchiveCollisionAppendsCounter(t *testing.T) {
	frozen := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	w := newTestWriter(t, func() time.Time { return frozen })

	ts := frozen.Format("2006-01-02T150405Z")
	canonical := filepath.Join(w.dir, "audit."+ts+".jsonl")
	first := filepath.Join(w.dir, "audit."+ts+".1.jsonl")
	if err := os.WriteFile(canonical, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed canonical: %v", err)
	}
	if err := os.WriteFile(first, []byte("y"), 0o600); err != nil {
		t.Fatalf("seed .1: %v", err)
	}
	if err := os.WriteFile(w.path, []byte("z"), 0o600); err != nil {
		t.Fatalf("seed live: %v", err)
	}

	if err := w.rotateLockedAtomic(""); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	expected := filepath.Join(w.dir, "audit."+ts+".2.jsonl")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected collision-avoidance archive at %q: %v", expected, err)
	}
}
