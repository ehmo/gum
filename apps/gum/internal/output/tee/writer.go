package tee

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ArtifactDate formats a tee artifact directory's <YYYY-MM-DD> component in
// UTC, matching the path layout in spec §9.0 line 1846.
func ArtifactDate(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// ArtifactPath returns the absolute path of the tee artifact for a single
// (profile, day, op, hash) tuple:
//
//	<profileDir>/tee/<YYYY-MM-DD>/<op_id>/<hash>.json.gz
//
// Caller is responsible for ensuring the leading directories exist (Write
// does this).
func ArtifactPath(profileDir string, day time.Time, opID, hash string) string {
	return filepath.Join(profileDir, "tee", ArtifactDate(day), opID, hash+".json.gz")
}

// ArtifactDir returns the per-op directory under which all artifacts for a
// given (profile, day, op) live. Useful for §9 lifecycle point 4 reverse-
// lookup scans by gum-uuh.
func ArtifactDir(profileDir string, day time.Time, opID string) string {
	return filepath.Join(profileDir, "tee", ArtifactDate(day), opID)
}

// Write persists the payload as a gzip-compressed JSON artifact at
// ArtifactPath. The directory chain is created at mode 700 (matching the
// profile dir), the file at mode 600 (spec §9.0 "mode 600 (user-only)"). The
// write is atomic via a temp-file + rename in the destination directory.
// Returns the absolute path written.
//
// payload is gzip-compressed in-memory before the rename so partial writes
// are never observable; size of payload is bounded by the upstream response
// envelope, which is already bounded by §3.1 limits.
func Write(profileDir string, day time.Time, opID, hash string, payload []byte) (string, error) {
	dir := ArtifactDir(profileDir, day, opID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("tee: mkdir %s: %w", dir, err)
	}
	dstPath := filepath.Join(dir, hash+".json.gz")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(payload); err != nil {
		_ = gz.Close()
		return "", fmt.Errorf("tee: gzip write: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("tee: gzip close: %w", err)
	}

	if err := atomicWrite(dir, ".tee-artifact.*.gz", dstPath, buf.Bytes(), 0o600, "artifact"); err != nil {
		return "", err
	}
	return dstPath, nil
}

// Read decompresses an artifact at the given absolute path. Returns
// fs.ErrNotExist when the file is missing (caller maps to
// RESULT_ARTIFACT_EXPIRED).
func Read(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("tee: open artifact: %w", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("tee: gzip reader: %w", err)
	}
	// Cap decompression so a crafted gzip artifact in the tee dir cannot OOM the
	// process when read back (notably via the MCP gum://results/<hash> resource).
	// Artifacts are written from response bodies already capped at write time, so
	// a legitimate artifact is always well under this bound.
	const maxArtifactBytes = 64 << 20 // 64 MiB
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(gz, maxArtifactBytes+1))
	if err != nil {
		return nil, fmt.Errorf("tee: gzip read: %w", err)
	}
	if n > maxArtifactBytes {
		_ = gz.Close()
		return nil, fmt.Errorf("tee: artifact exceeds %d-byte cap", maxArtifactBytes)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("tee: gzip close: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteJSON marshals v as JSON and persists it via Write. Returns the
// destination path.
func WriteJSON(profileDir string, day time.Time, opID, hash string, v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("tee: marshal payload: %w", err)
	}
	return Write(profileDir, day, opID, hash, raw)
}

// FindArtifact performs the spec §9 lifecycle point 4 reverse-lookup:
// directory-scans <profileDir>/tee/ for a file named <hash>.json.gz under
// any <YYYY-MM-DD>/<op_id>/ subtree. The scan window is bounded by the
// caller (maxDays); v0.1.0 callers pass the configured tee_retention_hours
// rounded up to whole days. Returns (path, true) on the first hit, ("",
// false) when no match exists.
//
// Scanning is O(days × ops × artifacts) in the worst case; with a 24-hour
// default retention this is effectively bounded to one or two date dirs.
// A sidecar BoltDB index is deferred to v0.3.0 per spec §9 lifecycle 4.
func FindArtifact(profileDir, hash string, maxDays int) (string, bool, error) {
	if hash == "" || maxDays < 1 {
		return "", false, nil
	}
	root := filepath.Join(profileDir, "tee")
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("tee: stat tee root: %w", err)
	}
	target := hash + ".json.gz"

	// Walk only the most-recent maxDays date directories (sorted desc).
	dateEntries, err := os.ReadDir(root)
	if err != nil {
		return "", false, fmt.Errorf("tee: read tee root: %w", err)
	}
	// Filter to YYYY-MM-DD dirs and sort newest first.
	var dates []string
	for _, e := range dateEntries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) != 10 || name[4] != '-' || name[7] != '-' {
			continue
		}
		dates = append(dates, name)
	}
	// Date strings are lexicographically sortable; sort newest first.
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if len(dates) > maxDays {
		dates = dates[:maxDays]
	}
	for _, d := range dates {
		dayDir := filepath.Join(root, d)
		opEntries, err := os.ReadDir(dayDir)
		if err != nil {
			continue
		}
		for _, op := range opEntries {
			if !op.IsDir() {
				continue
			}
			candidate := filepath.Join(dayDir, op.Name(), target)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, true, nil
			}
		}
	}
	return "", false, nil
}
