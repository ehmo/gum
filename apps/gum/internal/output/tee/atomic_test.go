package tee

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubFile is a fileWriter that fails on configured ops. Useful for
// exercising every error branch of atomicWrite without a hostile FS.
type stubFile struct {
	name      string
	writeErr  error
	chmodErr  error
	closeErr  error
	closeRan  bool
	chmodRan  bool
	writeRan  bool
}

func (s *stubFile) Name() string { return s.name }
func (s *stubFile) Write(p []byte) (int, error) {
	s.writeRan = true
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return len(p), nil
}
func (s *stubFile) Chmod(_ os.FileMode) error {
	s.chmodRan = true
	return s.chmodErr
}
func (s *stubFile) Close() error {
	s.closeRan = true
	return s.closeErr
}

// withHooks swaps the three indirection points for the duration of the
// returned cleanup func. Use with t.Cleanup.
func withHooks(t *testing.T,
	openFn func(dir, pattern string) (fileWriter, error),
	rename func(string, string) error,
	remove func(string) error,
) {
	t.Helper()
	if openFn != nil {
		prev := openTempFn
		openTempFn = openFn
		t.Cleanup(func() { openTempFn = prev })
	}
	if rename != nil {
		prev := renameFn
		renameFn = rename
		t.Cleanup(func() { renameFn = prev })
	}
	if remove != nil {
		prev := removeFn
		removeFn = remove
		t.Cleanup(func() { removeFn = prev })
	}
}

func TestAtomicWriteCreateTempError(t *testing.T) {
	withHooks(t,
		func(dir, pattern string) (fileWriter, error) {
			return nil, errors.New("simulated CreateTemp failure")
		}, nil, nil)
	err := atomicWrite(t.TempDir(), ".x.*", filepath.Join(t.TempDir(), "dst"), []byte("hi"), 0o600, "thing")
	if err == nil || !strings.Contains(err.Error(), "create temp thing") {
		t.Fatalf("expected create-temp error wrapper, got %v", err)
	}
}

func TestAtomicWriteWriteError(t *testing.T) {
	var removed string
	withHooks(t,
		func(dir, pattern string) (fileWriter, error) {
			return &stubFile{name: filepath.Join(dir, "tmpfile"), writeErr: errors.New("disk full")}, nil
		},
		nil,
		func(p string) error { removed = p; return nil },
	)
	err := atomicWrite(t.TempDir(), ".x.*", "/dst", []byte("hi"), 0o600, "thing")
	if err == nil || !strings.Contains(err.Error(), "write thing") {
		t.Fatalf("expected write-thing error, got %v", err)
	}
	if removed == "" || !strings.HasSuffix(removed, "tmpfile") {
		t.Errorf("temp file was not cleaned up; removed=%q", removed)
	}
}

func TestAtomicWriteChmodError(t *testing.T) {
	var removed string
	sf := &stubFile{name: "/tmp/fakefile", chmodErr: errors.New("perm denied")}
	withHooks(t,
		func(dir, pattern string) (fileWriter, error) { return sf, nil },
		nil,
		func(p string) error { removed = p; return nil },
	)
	err := atomicWrite(t.TempDir(), ".x.*", "/dst", []byte("hi"), 0o600, "thing")
	if err == nil || !strings.Contains(err.Error(), "chmod thing") {
		t.Fatalf("expected chmod-thing error, got %v", err)
	}
	if !sf.closeRan {
		t.Errorf("temp file must be closed on chmod error")
	}
	if removed != "/tmp/fakefile" {
		t.Errorf("temp file not removed; removed=%q", removed)
	}
}

func TestAtomicWriteCloseError(t *testing.T) {
	var removed string
	sf := &stubFile{name: "/tmp/fakefile", closeErr: errors.New("close failed")}
	withHooks(t,
		func(dir, pattern string) (fileWriter, error) { return sf, nil },
		nil,
		func(p string) error { removed = p; return nil },
	)
	err := atomicWrite(t.TempDir(), ".x.*", "/dst", []byte("hi"), 0o600, "thing")
	if err == nil || !strings.Contains(err.Error(), "close thing") {
		t.Fatalf("expected close-thing error, got %v", err)
	}
	if removed != "/tmp/fakefile" {
		t.Errorf("temp file not removed; removed=%q", removed)
	}
}

func TestAtomicWriteRenameError(t *testing.T) {
	var removed string
	sf := &stubFile{name: "/tmp/fakefile"}
	withHooks(t,
		func(dir, pattern string) (fileWriter, error) { return sf, nil },
		func(_, _ string) error { return errors.New("rename failed") },
		func(p string) error { removed = p; return nil },
	)
	err := atomicWrite(t.TempDir(), ".x.*", "/dst", []byte("hi"), 0o600, "thing")
	if err == nil || !strings.Contains(err.Error(), "rename thing") {
		t.Fatalf("expected rename-thing error, got %v", err)
	}
	if removed != "/tmp/fakefile" {
		t.Errorf("temp file not removed after rename failure; removed=%q", removed)
	}
}

func TestAtomicWriteSuccess(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "final.txt")
	if err := atomicWrite(dir, ".x.*", dst, []byte("payload"), 0o600, "thing"); err != nil {
		t.Fatalf("atomicWrite success path: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Errorf("dst content = %q, want %q", string(data), "payload")
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("dst mode = %o, want 0600", info.Mode().Perm())
	}
}
