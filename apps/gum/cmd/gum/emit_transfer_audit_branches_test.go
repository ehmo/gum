package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEmitTransferAuditEmptyProfileDirReturnsSilently pins the
// `profileDir == "" → return` early-out arm. The transfer has already
// committed to plugins.lock by the time emitTransferAudit runs, so a
// missing profile dir MUST NOT panic the caller — it just skips the
// audit row.
func TestEmitTransferAuditEmptyProfileDirReturnsSilently(t *testing.T) {
	// Smoke: no panic, no side-effect that could break a caller.
	emitTransferAudit("", "prefix", "old", "new", false)
}

// TestEmitTransferAuditAuditlogNewErrorReturnsSilently pins the
// `auditlog.New err → return` arm. auditlog.New calls MkdirAll on the
// profileDir; if a regular file already occupies that path, MkdirAll
// returns ENOTDIR and emitTransferAudit MUST swallow the error rather
// than crash a transfer that's already committed on disk.
func TestEmitTransferAuditAuditlogNewErrorReturnsSilently(t *testing.T) {
	tmp := t.TempDir()
	// Plant a regular file where the profile dir is supposed to live.
	blocker := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}

	// Should swallow the MkdirAll-ENOTDIR error and return silently.
	emitTransferAudit(blocker, "prefix", "old", "new", true)

	// Verify no audit.jsonl materialised next to the blocker.
	if _, err := os.Stat(filepath.Join(blocker, "audit.jsonl")); err == nil {
		t.Error("audit.jsonl created under regular-file profileDir; expected silent skip")
	}
}
