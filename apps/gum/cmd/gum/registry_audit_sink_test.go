package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec §8.7 sends the fsync fallback warning to the host log and the audit log.
// The registry raises it through an injected sink (§14 keeps internal/auditlog
// out of internal/plugins/registry); this pins the cmd side of that seam
// (gum-452w).
func TestRegistryAuditSinkWritesProfileAuditRow(t *testing.T) {
	dir := t.TempDir()
	sink := registryAuditSink{profileDir: dir}

	sink.Append(map[string]any{
		"event_type":    "fsync_not_supported",
		"path":          dir,
		"syscall_errno": "ENOTSUP",
	})

	raw, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	var row map[string]any
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(raw)), "\n", 2)[0])
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		t.Fatalf("audit row is not JSON: %v (%q)", err, line)
	}
	if row["event_type"] != "fsync_not_supported" {
		t.Errorf("event_type = %v; want fsync_not_supported", row["event_type"])
	}
	if row["syscall_errno"] != "ENOTSUP" {
		t.Errorf("syscall_errno = %v; want ENOTSUP", row["syscall_errno"])
	}
}

// An unresolved profile dir must not create files anywhere.
func TestRegistryAuditSinkSkipsEmptyProfileDir(t *testing.T) {
	registryAuditSink{}.Append(map[string]any{"event_type": "fsync_not_supported"})
}
