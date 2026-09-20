package registry

import (
	"context"
	"os"
	"sync"
	"syscall"
	"testing"
)

// recordingSink captures the audit rows the registry emits. Production wires
// this seam to internal/auditlog from cmd; registry must not import it (§14).
type recordingSink struct {
	mu   sync.Mutex
	rows []map[string]any
}

func (s *recordingSink) Append(entry map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, entry)
}

func (s *recordingSink) snapshot() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.rows...)
}

// Spec §8.7 "Filesystem fsync fallback" sends the warning to the host log AND
// the audit log, "so the loss of crash-safety guarantees is auditable".
// 7644052 landed only the slog leg (gum-452w).
func TestUnsupportedFsyncWritesAuditRow(t *testing.T) {
	dir := t.TempDir()
	sink := &recordingSink{}

	r := New(dir).WithAuditSink(sink)
	r.syncDirFn = func(string) error { return fsyncUnsupportedError{err: syscall.ENOTSUP} }
	r.syncFileFn = func(*os.File) error { return syscall.EINVAL }

	if err := r.WriteTransaction(context.Background(), addPlugin("acme")); err != nil {
		t.Fatalf("transaction failed on an fsync-unsupported filesystem: %v", err)
	}

	rows := sink.snapshot()
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d; want exactly 1 fsync_not_supported row", len(rows))
	}
	row := rows[0]
	if row["event_type"] != "fsync_not_supported" {
		t.Errorf("event_type = %v; want fsync_not_supported", row["event_type"])
	}
	if row["path"] != dir {
		t.Errorf("path = %v; want %s", row["path"], dir)
	}
	if row["syscall_errno"] == nil || row["syscall_errno"] == "unknown" {
		t.Errorf("syscall_errno = %v; want a named errno", row["syscall_errno"])
	}
	if row["suggestion"] == nil {
		t.Error("suggestion missing; the operator needs the remediation text")
	}

	// Detection is once per profile per process, so the audit leg must not
	// repeat either.
	if err := r.WriteTransaction(context.Background(), addPlugin("beta")); err != nil {
		t.Fatalf("second transaction: %v", err)
	}
	if got := len(sink.snapshot()); got != 1 {
		t.Errorf("audit rows after a second transaction = %d; want 1", got)
	}
}
