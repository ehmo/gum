//go:build !windows

package auditlog_test

// Hard-ceiling behaviour under cross-process rotation-lock contention
// (test-matrix row 162, bead gum-qq6m).
//
// Spec §11 gives the ceiling append two permitted outcomes and forbids a
// third: it blocks until the emergency rotation succeeds, or it reports an
// audit append failure. It never writes past the cap. Every other
// hard-ceiling test rotates with the lock free, so the branch that decides
// between those two outcomes was never entered.
//
// The contention is real, not simulated: the test opens audit.jsonl.lock
// itself and takes an exclusive flock on it. flock is held per open file
// description, so a second descriptor inside this process contends with the
// writer exactly as a second process would.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/auditlog"
)

const contentionCeilingBytes = 500

// holdRotationLock takes the writer's advisory lock and returns a release
// func. The lock is taken before the writer ever reaches for it, so the
// writer's acquire always finds it held.
func holdRotationLock(t *testing.T, dir string) func() {
	t.Helper()

	f, err := os.OpenFile(filepath.Join(dir, "audit.jsonl.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open rotation lock: %v", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		t.Fatalf("flock rotation lock: %v", err)
	}

	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
	t.Cleanup(release)
	return release
}

// fileSize returns the size of path, or 0 when it is absent.
func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// archiveCount counts rotated audit files in dir.
func archiveCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "audit.") && strings.HasSuffix(name, ".jsonl") && name != "audit.jsonl" {
			n++
		}
	}
	return n
}

// fillToCeiling appends until one more entry would cross the ceiling, so the
// append the test then makes is the one that triggers emergency rotation. It
// returns the live file's size.
func fillToCeiling(t *testing.T, w *auditlog.Writer, path string) int64 {
	t.Helper()

	w.Append(makeEntry("fill"))
	entryLen := fileSize(t, path)
	if entryLen == 0 {
		t.Fatal("the first append wrote nothing")
	}

	size := entryLen
	for size+entryLen <= contentionCeilingBytes {
		w.Append(makeEntry("fill"))
		grown := fileSize(t, path)
		if grown <= size {
			t.Fatalf("append wrote nothing at size %d", size)
		}
		size = grown
	}
	if n := archiveCount(t, dirOf(path)); n != 0 {
		t.Fatalf("filling already rotated %d time(s); the ceiling append must be the first", n)
	}
	return size
}

// dirOf is filepath.Dir, named so the fill helper reads as one line.
func dirOf(path string) string { return filepath.Dir(path) }

func TestAuditHardCeilingContention(t *testing.T) {
	t.Run("blocks_until_the_lock_is_released", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "audit.jsonl")
		w, err := auditlog.New(dir,
			auditlog.WithMaxSizeBytes(0), // isolate the ceiling path
			auditlog.WithHardCeilingBytes(contentionCeilingBytes),
			auditlog.WithHardCeilingLockTimeout(10*time.Second),
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		filled := fillToCeiling(t, w, path)

		release := holdRotationLock(t, dir)

		done := make(chan struct{})
		go func() {
			defer close(done)
			w.Append(makeEntry("ceiling"))
		}()

		select {
		case <-done:
			t.Fatal("the ceiling append finished while the rotation lock was held; it must block until rotation can run")
		case <-time.After(250 * time.Millisecond):
		}
		if got := fileSize(t, path); got != filled {
			t.Fatalf("audit.jsonl grew from %d to %d while blocked; nothing may be written before rotation", filled, got)
		}

		release()

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the ceiling append never finished after the rotation lock was released")
		}

		if n := archiveCount(t, dir); n != 1 {
			t.Errorf("archives after the blocked rotation = %d; want 1", n)
		}
		if got := fileSize(t, path); got > contentionCeilingBytes {
			t.Errorf("audit.jsonl is %d bytes; the cap is %d", got, contentionCeilingBytes)
		}
		if got := fileSize(t, path); got == 0 {
			t.Error("the entry was dropped; the append must land in the fresh file after rotation")
		}
		if _, err := os.Stat(filepath.Join(dir, "audit.broken")); err == nil {
			t.Error("audit.broken was written; the append succeeded, so there is no failure to record")
		}
	})

	t.Run("fails_the_append_rather_than_exceeding_the_cap", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "audit.jsonl")
		w, err := auditlog.New(dir,
			auditlog.WithMaxSizeBytes(0),
			auditlog.WithHardCeilingBytes(contentionCeilingBytes),
			auditlog.WithHardCeilingLockTimeout(200*time.Millisecond),
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		filled := fillToCeiling(t, w, path)

		holdRotationLock(t, dir) // held for the rest of the subtest

		w.Append(makeEntry("ceiling"))

		if got := fileSize(t, path); got != filled {
			t.Errorf("audit.jsonl went from %d to %d bytes; a ceiling append that cannot rotate must write nothing", filled, got)
		}
		if n := archiveCount(t, dir); n != 0 {
			t.Errorf("archives = %d; rotation could not run, so there is nothing to archive", n)
		}

		sentinel, err := os.ReadFile(filepath.Join(dir, "audit.broken"))
		if err != nil {
			t.Fatalf("audit.broken is missing; a refused append is an audit append failure and §11 records it: %v", err)
		}
		if !strings.Contains(string(sentinel), "hard-ceiling rotate") {
			t.Errorf("audit.broken says %q; it must name the hard-ceiling rotation that failed", strings.TrimSpace(string(sentinel)))
		}
	})
}
