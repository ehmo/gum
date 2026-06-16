// Tests for the audit-log writer (spec §11). Covers the normative entry
// shape, size-based rotation, max_files retention, and the audit.broken
// sentinel-write on failure.

package auditlog_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/auditlog"
)

// TestAuditEntryShape verifies that one Append produces a JSONL line whose
// first key is `v` with value 1 and that the spec's required keys are
// present in the right order. Per §11, optional false/null fields are
// omitted to keep the line compact.
func TestAuditEntryShape(t *testing.T) {
	dir := t.TempDir()
	w, err := auditlog.New(dir, auditlog.WithClock(func() time.Time {
		return time.Date(2026, 5, 23, 14, 30, 0, 123000000, time.UTC)
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	w.Append(map[string]any{
		"op_id":      "gmail.users.messages.list",
		"variant_id": "gmail.v1.rest.users.messages.list",
		"args_hash":  "abcd1234",
		"client_id":  "cli",
		"risk_class": "read",
		// optional risk_override defaults to false → still emitted per §11
	})

	data, err := os.ReadFile(w.Path())
	if err != nil {
		t.Fatalf("read audit.jsonl: %v", err)
	}
	line := strings.TrimRight(string(data), "\n")
	if !strings.HasPrefix(line, `{"v":1,`) {
		t.Errorf("first key must be \"v\":1; got line: %s", line)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("parse JSONL: %v", err)
	}
	required := []string{"v", "ts", "op_id", "variant_id", "args_hash", "client_id", "risk_class", "risk_override"}
	for _, k := range required {
		if _, ok := parsed[k]; !ok {
			t.Errorf("required key %q missing from entry: %s", k, line)
		}
	}
	if v, _ := parsed["v"].(float64); v != 1 {
		t.Errorf("v=%v; want 1", parsed["v"])
	}
	if _, present := parsed["shaping_bypassed"]; present {
		t.Errorf("optional false key shaping_bypassed must be omitted; line: %s", line)
	}
	if _, present := parsed["dual_fetch"]; present {
		t.Errorf("optional false key dual_fetch must be omitted; line: %s", line)
	}
	if ts, _ := parsed["ts"].(string); ts != "2026-05-23T14:30:00.123Z" {
		t.Errorf("ts=%q; want 2026-05-23T14:30:00.123Z", ts)
	}
}

// TestAuditEntryShapeEmitsConditionalKeysWhenTrue verifies that the
// "compact" rule (omit when false) emits the key when the flag is true.
func TestAuditEntryShapeEmitsConditionalKeysWhenTrue(t *testing.T) {
	dir := t.TempDir()
	w, _ := auditlog.New(dir)
	w.Append(map[string]any{
		"op_id":                "x.y.z",
		"variant_id":           "x.v1.rest.y.z",
		"args_hash":            "h",
		"client_id":            "mcp",
		"risk_class":           "write",
		"risk_override":        true,
		"risk_override_reason": "vendor mislabels as read",
		"shaping_bypassed":     true,
		"dual_fetch":           true,
	})
	data, _ := os.ReadFile(w.Path())
	line := strings.TrimRight(string(data), "\n")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"risk_override", "risk_override_reason", "shaping_bypassed", "dual_fetch"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("expected key %q present; line: %s", k, line)
		}
	}
}

// TestAuditLogRotation forces a small max_size, writes many entries, and
// asserts the file rotates with an archive matching `audit.<iso>.jsonl`.
// max_files retention is exercised by writing past the cap.
func TestAuditLogRotation(t *testing.T) {
	dir := t.TempDir()
	// 200 bytes is enough for one entry; force rotation after each Append.
	w, err := auditlog.New(dir, auditlog.WithMaxSizeBytes(200), auditlog.WithMaxFiles(2))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i := 0; i < 5; i++ {
		w.Append(map[string]any{
			"op_id":      "x.y.z",
			"variant_id": "x.v1",
			"args_hash":  "h",
			"client_id":  "cli",
			"risk_class": "read",
		})
		// Give the rotation timestamp a fresh second.
		time.Sleep(1100 * time.Millisecond)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	archives := 0
	hasLive := false
	for _, e := range entries {
		name := e.Name()
		if name == "audit.jsonl" {
			hasLive = true
			continue
		}
		if strings.HasPrefix(name, "audit.") && strings.HasSuffix(name, ".jsonl") {
			archives++
		}
	}
	if !hasLive {
		t.Error("expected audit.jsonl to exist after rotation; got none")
	}
	if archives > 2 {
		t.Errorf("max_files=2 not enforced: found %d archives", archives)
	}
	if archives == 0 {
		t.Errorf("expected at least one rotated archive; found %d", archives)
	}

	// Each archive must be mode 600 per spec §11.
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "audit.") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, _ := os.Stat(filepath.Join(dir, name))
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("file %q mode=%o; want 0600", name, mode)
		}
	}
}

// TestAuditBrokenSentinelOnWriteFailure removes write permissions from the
// profile directory, forces an Append, and asserts the sentinel is written
// (the sentinel write itself goes to a path the writer controls; for a
// read-only dir the sentinel write also fails but the test verifies the
// failure path engages at all by checking the slog event surface).
//
// We simulate the failure by pre-creating audit.jsonl as a directory so the
// O_APPEND open fails with EISDIR. The sentinel write to audit.broken
// succeeds because it's in the parent dir.
func TestAuditBrokenSentinelOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	// Sabotage audit.jsonl by creating it as a directory.
	if err := os.Mkdir(filepath.Join(dir, "audit.jsonl"), 0o700); err != nil {
		t.Fatalf("mkdir trap: %v", err)
	}
	w, err := auditlog.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.Append(map[string]any{
		"op_id":      "x",
		"variant_id": "x.v1",
		"args_hash":  "h",
		"client_id":  "cli",
		"risk_class": "read",
	})
	sentinel := filepath.Join(dir, "audit.broken")
	info, err := os.Stat(sentinel)
	if err != nil {
		t.Fatalf("expected audit.broken sentinel, stat err: %v", err)
	}
	if info.Size() == 0 {
		t.Error("audit.broken is empty; spec §11 requires timestamp + OS error")
	}
}

// TestSentinelClearedOnSuccessfulAppend verifies that after a failed append
// produced audit.broken, a subsequent successful append clears it.
func TestSentinelClearedOnSuccessfulAppend(t *testing.T) {
	dir := t.TempDir()
	// Pre-seed the sentinel as if a prior failure happened.
	sentinel := filepath.Join(dir, "audit.broken")
	if err := os.WriteFile(sentinel, []byte("prior failure"), 0o600); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}
	w, err := auditlog.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w.Append(map[string]any{
		"op_id":      "x",
		"variant_id": "x.v1",
		"args_hash":  "h",
		"client_id":  "cli",
		"risk_class": "read",
	})
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Errorf("sentinel should be cleared after successful append; stat err: %v", err)
	}
}

// TestAuditGracefulShutdown — bead-named acceptance for gum-dxpy (spec §11
// "graceful-shutdown drain"). Covers three branches:
//
//  1. drain-success: Close() flushes all queued entries within the drain
//     timeout; emitted file has the expected line count; DrainedCount
//     matches; DroppedCount is zero; audit.broken sentinel is NOT written.
//  2. drain-timeout: a slow-write profile blocks the worker so the drain
//     timeout fires; remaining queued entries are dropped (NOT persisted,
//     NOT counted as audit.broken).
//  3. overflow-drop: pushing past the channel capacity counts as dropped.
//
// Test-matrix row 162. Wired to cmd/gum's SIGTERM/SIGINT handler.
func TestAuditGracefulShutdown(t *testing.T) {
	t.Run("drain_success", func(t *testing.T) {
		dir := t.TempDir()
		w, err := auditlog.New(dir,
			auditlog.WithBufferedChannel(64),
			auditlog.WithDrainTimeout(2*time.Second),
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		const n = 20
		for i := 0; i < n; i++ {
			w.Append(map[string]any{
				"op_id":      "drain.success",
				"variant_id": "drain.success.v1",
				"args_hash":  "h",
				"client_id":  "cli",
				"risk_class": "read",
			})
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		// All entries drained; nothing dropped; sentinel absent.
		if got := w.DrainedCount(); got != n {
			t.Errorf("DrainedCount = %d; want %d", got, n)
		}
		if got := w.DroppedCount(); got != 0 {
			t.Errorf("DroppedCount = %d; want 0", got)
		}
		if _, err := os.Stat(w.SentinelPath()); !os.IsNotExist(err) {
			t.Errorf("audit.broken sentinel created during graceful drain: stat err=%v", err)
		}
		// And the file has n lines.
		f, err := os.Open(w.Path())
		if err != nil {
			t.Fatalf("open audit.jsonl: %v", err)
		}
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		lines := 0
		for scanner.Scan() {
			lines++
		}
		if lines != n {
			t.Errorf("audit.jsonl line count = %d; want %d", lines, n)
		}
	})

	t.Run("drain_timeout_drops_remainder", func(t *testing.T) {
		dir := t.TempDir()
		// Tiny drain timeout so the worker can never finish before we cancel.
		// We push enough entries that the worker is still draining when the
		// 1ms timeout fires; the rest are counted as dropped.
		w, err := auditlog.New(dir,
			auditlog.WithBufferedChannel(1024),
			auditlog.WithDrainTimeout(1*time.Millisecond),
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		const n = 1024
		for i := 0; i < n; i++ {
			w.Append(map[string]any{
				"op_id":      "drain.timeout",
				"variant_id": "drain.timeout.v1",
				"args_hash":  "h",
				"client_id":  "cli",
				"risk_class": "read",
			})
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		drained := w.DrainedCount()
		dropped := w.DroppedCount()
		if drained+dropped != n {
			t.Errorf("drained(%d) + dropped(%d) = %d; want %d", drained, dropped, drained+dropped, n)
		}
		if dropped == 0 {
			t.Errorf("DroppedCount = 0; want >0 (drain timeout was 1ms over %d entries)", n)
		}
		// audit.broken MUST NOT be created (spec §11 explicit "silently dropped").
		if _, err := os.Stat(w.SentinelPath()); !os.IsNotExist(err) {
			t.Errorf("audit.broken sentinel created on drain timeout: stat err=%v (spec §11 forbids)", err)
		}
	})

	t.Run("overflow_drop_when_channel_full", func(t *testing.T) {
		dir := t.TempDir()
		// Capacity=1 channel + a worker that will start the first write but
		// not yet finish. The second Append should land while the channel
		// holds the first entry → counted as dropped via the non-blocking
		// select default branch. (We don't strictly need the worker to be
		// slow: as long as we enqueue faster than it can drain, drops occur.)
		w, err := auditlog.New(dir, auditlog.WithBufferedChannel(1))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		// Saturate: push 1000 entries into a 1-slot channel. Some drops are
		// guaranteed.
		const n = 1000
		for i := 0; i < n; i++ {
			w.Append(map[string]any{
				"op_id":      "overflow",
				"variant_id": "overflow.v1",
				"args_hash":  "h",
				"client_id":  "cli",
				"risk_class": "read",
			})
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		drained := w.DrainedCount()
		dropped := w.DroppedCount()
		if dropped == 0 {
			t.Errorf("DroppedCount = 0; want >0 with channel cap=1 + %d enqueues", n)
		}
		if drained+dropped != n {
			t.Errorf("drained(%d) + dropped(%d) = %d; want %d", drained, dropped, drained+dropped, n)
		}
	})

	t.Run("close_is_idempotent", func(t *testing.T) {
		dir := t.TempDir()
		w, err := auditlog.New(dir, auditlog.WithBufferedChannel(8))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		w.Append(map[string]any{
			"op_id":      "idem",
			"variant_id": "idem.v1",
			"args_hash":  "h",
			"client_id":  "cli",
			"risk_class": "read",
		})
		if err := w.Close(); err != nil {
			t.Fatalf("first Close: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("second Close: %v", err)
		}
		// After Close, Append must not panic and MUST count as dropped.
		w.Append(map[string]any{"op_id": "after_close"})
		if got := w.DroppedCount(); got < 1 {
			t.Errorf("DroppedCount = %d; want ≥1 (post-Close Append must drop)", got)
		}
	})

	t.Run("close_without_buffered_channel_is_noop", func(t *testing.T) {
		// Sync-mode writer: Close MUST be a no-op (no goroutine to drain) and
		// MUST NOT emit audit_drain_complete.
		dir := t.TempDir()
		w, err := auditlog.New(dir) // no WithBufferedChannel → sync mode
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Errorf("Close on sync writer: %v", err)
		}
		if got := w.DrainedCount(); got != 0 {
			t.Errorf("DrainedCount on sync writer = %d; want 0", got)
		}
	})

	t.Run("drain_timeout_clamping", func(t *testing.T) {
		// Negative + above-max values clamp to the documented bounds. We
		// don't assert exact behaviour beyond "Close doesn't hang"; the
		// invariant is that a misconfigured value never blocks indefinitely.
		dir := t.TempDir()
		w, err := auditlog.New(dir,
			auditlog.WithBufferedChannel(4),
			auditlog.WithDrainTimeout(-5*time.Second), // negative → DefaultDrainTimeout
		)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		w.Append(map[string]any{"op_id": "clamp"})
		closeStart := time.Now()
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if elapsed := time.Since(closeStart); elapsed > 5*time.Second {
			t.Errorf("Close took %v; expected to drain quickly under DefaultDrainTimeout", elapsed)
		}
	})
}

// TestAppendIsConcurrentSafe spawns many goroutines hammering Append and
// asserts the resulting file has one valid JSONL record per call.
func TestAppendIsConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	w, err := auditlog.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	const n = 50
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			w.Append(map[string]any{
				"op_id":      "x",
				"variant_id": "x.v1",
				"args_hash":  "h",
				"client_id":  "cli",
				"risk_class": "read",
			})
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	f, err := os.Open(w.Path())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() {
		var parsed map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &parsed); err != nil {
			t.Errorf("line %d invalid JSON: %v", lines, err)
		}
		lines++
	}
	if lines != n {
		t.Errorf("expected %d lines, got %d", n, lines)
	}
}
