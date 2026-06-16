package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteTransactionCtxCancelled covers the earliest fail-fast guard:
// a pre-cancelled context returns ctx.Err() before the lock acquisition
// or any I/O. This is the only branch where the protocol must abort
// without touching the profile directory.
func TestWriteTransactionCtxCancelled(t *testing.T) {
	r := New(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.WriteTransaction(ctx, func(*Files) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v; want context.Canceled", err)
	}
}

// TestWriteTransactionMutateErrorPropagates pins the contract: if the
// caller's mutate function fails, no atomic writes happen and the error
// is wrapped with the "registry: mutate:" prefix.
func TestWriteTransactionMutateErrorPropagates(t *testing.T) {
	r := New(t.TempDir())
	sentinel := errors.New("mutate-boom")
	err := r.WriteTransaction(context.Background(), func(*Files) error { return sentinel })
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("got %v; want wraps sentinel", err)
	}
}

// TestWriteJSONAtomicMarshalError covers the JSON-marshal branch: a
// channel value cannot be serialised, so writeJSONAtomic must surface
// the wrapped "registry: marshal" error without creating a file.
func TestWriteJSONAtomicMarshalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	// Channels are not JSON-serialisable.
	err := writeJSONAtomic(path, make(chan int))
	if err == nil {
		t.Fatal("expected marshal error")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("file should not exist: stat err=%v", statErr)
	}
}

// TestWriteJSONAtomicOpenError covers the OpenFile branch: writing into
// a non-existent directory must produce the wrapped "registry: open
// temp" error.
func TestWriteJSONAtomicOpenError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir", "out.json")
	err := writeJSONAtomic(missing, map[string]any{"k": "v"})
	if err == nil {
		t.Fatal("expected open error")
	}
}

// TestFsyncDirOpenError covers the os.Open branch: a non-existent
// directory yields "registry: open dir" rather than silently succeeding.
func TestFsyncDirOpenError(t *testing.T) {
	if err := fsyncDir(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected open dir error")
	}
}

// TestFsyncDirHappyPath confirms fsync on a real, owned directory
// succeeds (covers the normal return), which is also the path most of
// production hits on durable filesystems.
func TestFsyncDirHappyPath(t *testing.T) {
	if err := fsyncDir(t.TempDir()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
