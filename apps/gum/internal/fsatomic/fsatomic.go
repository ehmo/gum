// Package fsatomic provides crash-safe file writes via the temp-file +
// rename pattern. A direct os.WriteFile can leave a truncated or zero-byte
// file if the process dies mid-write; for files gum reads back and validates
// (the confirmation signing key, the BM25 index, the audit sentinel) a partial
// write turns into a hard failure on the next start. Writing to a sibling temp
// file and renaming makes the replacement atomic on POSIX filesystems.
package fsatomic

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile writes data to path atomically: it creates a temp file in the same
// directory, writes and fsyncs it, chmods to mode, then renames over path. The
// parent directory must already exist. On any error the temp file is removed
// and path is left untouched.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gum-*.tmp")
	if err != nil {
		return fmt.Errorf("fsatomic: tempfile in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsatomic: write %s: %w", tmpPath, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsatomic: chmod %s: %w", tmpPath, err)
	}
	// fsync the data before the rename so a crash can't leave a renamed-but-empty
	// file (rename is atomic, but the data blocks may not be durable yet).
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsatomic: sync %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("fsatomic: close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("fsatomic: rename %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}
