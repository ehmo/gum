package fsatomic

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// faultFile wraps a real temp file and fails one chosen operation. The
// write/chmod/sync/close arms of WriteFile are not reachable through any
// portable filesystem state, so they are driven from here.
type faultFile struct {
	real     *os.File
	failOn   string
	err      error
	closes   int
	writeLen int
}

func (f *faultFile) Name() string { return f.real.Name() }

func (f *faultFile) Write(p []byte) (int, error) {
	if f.failOn == "write" {
		return f.writeLen, f.err
	}
	return f.real.Write(p)
}

func (f *faultFile) Chmod(mode os.FileMode) error {
	if f.failOn == "chmod" {
		return f.err
	}
	return f.real.Chmod(mode)
}

func (f *faultFile) Sync() error {
	if f.failOn == "sync" {
		return f.err
	}
	return f.real.Sync()
}

func (f *faultFile) Close() error {
	f.closes++
	if f.failOn == "close" {
		_ = f.real.Close()
		return f.err
	}
	return f.real.Close()
}

var errFault = errors.New("injected fault")

// withFault installs a createTemp that fails the named operation and restores
// the real one when the test ends. It returns the injected file so the test can
// assert the temp file was closed.
func withFault(t *testing.T, op string) *faultFile {
	t.Helper()
	var injected *faultFile
	prev := createTemp
	createTemp = func(dir, pattern string) (tempFile, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		injected = &faultFile{real: f, failOn: op, err: errFault}
		return injected, nil
	}
	t.Cleanup(func() { createTemp = prev })
	return injected
}

func TestWriteFileCreateTempError(t *testing.T) {
	prev := createTemp
	createTemp = func(string, string) (tempFile, error) { return nil, errFault }
	t.Cleanup(func() { createTemp = prev })

	err := WriteFile(filepath.Join(t.TempDir(), "k"), []byte("x"), 0o600)
	if !errors.Is(err, errFault) {
		t.Fatalf("WriteFile = %v, want errFault", err)
	}
	if !strings.Contains(err.Error(), "tempfile in") {
		t.Errorf("error text = %q, want it to name the failing step", err)
	}
}

func TestWriteFileFaultArmsCleanUp(t *testing.T) {
	for _, op := range []string{"write", "chmod", "sync", "close"} {
		t.Run(op, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "k")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}

			withFault(t, op)
			err := WriteFile(path, []byte("replacement"), 0o600)
			if !errors.Is(err, errFault) {
				t.Fatalf("WriteFile = %v, want errFault", err)
			}
			if !strings.Contains(err.Error(), op) {
				t.Errorf("error text = %q, want it to name %q", err, op)
			}

			// The target keeps its previous bytes and no temp file survives.
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("ReadFile: %v", readErr)
			}
			if string(got) != "original" {
				t.Errorf("target content = %q, want it untouched", got)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Errorf("dir has %d entries, want 1 (temp file leaked)", len(entries))
			}
		})
	}
}
