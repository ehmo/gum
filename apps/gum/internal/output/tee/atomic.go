package tee

import (
	"fmt"
	"os"
)

// fileWriter is the subset of *os.File methods used by atomicWrite. The
// abstraction exists so tests can inject errors at each step of the
// CreateTemp → Write → Chmod → Close → Rename pipeline (writer/secret
// share the same pattern and benefit from a single error-branch suite).
type fileWriter interface {
	Name() string
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Close() error
}

// Indirection points swapped by tee_inject_test.go to exercise every
// error path. Production paths always go through these vars; the unit
// tests assert each branch by replacing them with stubs and asserting
// no leak (tmp removed) on failure.
var (
	openTempFn = func(dir, pattern string) (fileWriter, error) {
		return os.CreateTemp(dir, pattern)
	}
	renameFn = os.Rename
	removeFn = os.Remove
)

// atomicWrite persists data to dstPath via the standard temp-file +
// rename idiom: CreateTemp in dir → Write → Chmod(mode) → Close → Rename.
// On any error the partially-written temp file is removed.
//
// errLabel is the user-facing operation prefix used in wrapped errors
// (e.g. "secret", "artifact") so the caller's logs stay legible.
func atomicWrite(dir, tempPattern, dstPath string, data []byte, mode os.FileMode, errLabel string) error {
	tmp, err := openTempFn(dir, tempPattern)
	if err != nil {
		return fmt.Errorf("tee: create temp %s: %w", errLabel, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = removeFn(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("tee: write %s: %w", errLabel, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("tee: chmod %s: %w", errLabel, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("tee: close %s: %w", errLabel, err)
	}
	if err := renameFn(tmpPath, dstPath); err != nil {
		cleanup()
		return fmt.Errorf("tee: rename %s: %w", errLabel, err)
	}
	return nil
}
