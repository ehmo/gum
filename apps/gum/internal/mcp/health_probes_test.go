package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestStatusHealthSubsystemEnum is the bead-named acceptance for gum-nb85:
// the gum://status/health resource MUST cover exactly the six closed-enum
// subsystems from spec §13 line 3149. Adding or removing a subsystem
// requires a minor-version spec PR plus a matching update to the test
// fixture below.
func TestStatusHealthSubsystemEnum(t *testing.T) {
	want := []string{
		"audit_log",
		"cache_sqlite",
		"canary_runner",
		"gain_ledger",
		"keychain",
		"tee_filesystem",
	}
	if len(staticHealthSubsystems) != len(want) {
		t.Fatalf("staticHealthSubsystems = %d entries; want %d (%v)", len(staticHealthSubsystems), len(want), want)
	}
	got := make(map[string]bool, len(staticHealthSubsystems))
	for _, s := range staticHealthSubsystems {
		got[s] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("staticHealthSubsystems missing %q", w)
		}
		if _, ok := healthProbes[w]; !ok {
			t.Errorf("healthProbes missing handler for %q", w)
		}
	}
	for k := range got {
		found := false
		for _, w := range want {
			if k == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("staticHealthSubsystems has unexpected entry %q (not in closed enum)", k)
		}
	}
	if len(healthProbes) != len(want) {
		t.Errorf("healthProbes has %d entries; want exactly %d (closed enum)", len(healthProbes), len(want))
	}
}

// TestHealthSnapshotReturnsAllSubsystems pins the contract that every probe
// fires and produces a row; the result MUST be sorted lexicographically so
// the TOON encoding is stable.
func TestHealthSnapshotReturnsAllSubsystems(t *testing.T) {
	var c healthSnapshotCache
	rows := c.snapshot(time.Now().UTC(), t.TempDir())
	if len(rows) != len(staticHealthSubsystems) {
		t.Fatalf("snapshot returned %d rows; want %d", len(rows), len(staticHealthSubsystems))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Subsystem >= rows[i].Subsystem {
			t.Errorf("rows not sorted: rows[%d].Subsystem=%q >= rows[%d].Subsystem=%q",
				i-1, rows[i-1].Subsystem, i, rows[i].Subsystem)
		}
	}
	for _, r := range rows {
		if r.Status != "healthy" && r.Status != "degraded" && r.Status != "down" {
			t.Errorf("subsystem %q status = %q; want one of healthy|degraded|down", r.Subsystem, r.Status)
		}
		if r.LastCheckAt.IsZero() {
			t.Errorf("subsystem %q LastCheckAt is zero", r.Subsystem)
		}
	}
}

// TestHealthSnapshotTTLCacheHit asserts the spec §13 line 3149 "5s sample
// TTL" — a second call within the window returns the same LastCheckAt
// values, proving the probes did not re-run.
func TestHealthSnapshotTTLCacheHit(t *testing.T) {
	var c healthSnapshotCache
	t0 := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	first := c.snapshot(t0, t.TempDir())
	second := c.snapshot(t0.Add(2*time.Second), t.TempDir())

	if len(first) != len(second) {
		t.Fatalf("snapshot lengths differ: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if !first[i].LastCheckAt.Equal(second[i].LastCheckAt) {
			t.Errorf("row %d (%s) LastCheckAt changed within TTL: first=%s second=%s",
				i, first[i].Subsystem, first[i].LastCheckAt, second[i].LastCheckAt)
		}
	}
}

// TestHealthSnapshotTTLCacheExpiry asserts that a call past the TTL window
// re-runs the probes, surfacing a fresh LastCheckAt.
func TestHealthSnapshotTTLCacheExpiry(t *testing.T) {
	var c healthSnapshotCache
	t0 := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	first := c.snapshot(t0, t.TempDir())
	tLater := t0.Add(healthSnapshotTTL + time.Second)
	second := c.snapshot(tLater, t.TempDir())

	for i := range first {
		if !second[i].LastCheckAt.Equal(tLater) {
			t.Errorf("row %d (%s) LastCheckAt = %s; want %s (refreshed past TTL)",
				i, second[i].Subsystem, second[i].LastCheckAt, tLater)
		}
	}
}

// TestProbeTeeFilesystemDetectsExistingTree verifies the tee probe upgrades
// its detail message when an artifact subtree already exists.
func TestProbeTeeFilesystemDetectsExistingTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "tee"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	row := probeTeeFilesystem(time.Now().UTC(), dir)
	if row.Status != "healthy" {
		t.Errorf("Status = %q; want healthy", row.Status)
	}
	if !strings.Contains(row.Detail, "tee tree present") {
		t.Errorf("Detail = %q; want to contain 'tee tree present'", row.Detail)
	}
}

// TestProbeTeeFilesystemEmptyProfile reports degraded when the active
// profile dir cannot be resolved (server constructed without HOME).
func TestProbeTeeFilesystemEmptyProfile(t *testing.T) {
	row := probeTeeFilesystem(time.Now().UTC(), "")
	if row.Status != "degraded" {
		t.Errorf("Status = %q; want degraded", row.Status)
	}
}

func TestProbeAuditLogEmptyProfile(t *testing.T) {
	row := probeAuditLog(time.Now().UTC(), "")
	if row.Status != "degraded" {
		t.Errorf("Status = %q; want degraded", row.Status)
	}
	if !strings.Contains(row.Detail, "unresolvable") {
		t.Errorf("Detail = %q; want unresolvable", row.Detail)
	}
}

func TestProbeAuditLogBrokenSentinel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "audit.broken"), []byte("write failed"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	row := probeAuditLog(time.Now().UTC(), dir)
	if row.Status != "degraded" {
		t.Errorf("Status = %q; want degraded", row.Status)
	}
	if !strings.Contains(row.Detail, "write failed") {
		t.Errorf("Detail = %q; want sentinel content", row.Detail)
	}
}

func TestProbeAuditLogWritable(t *testing.T) {
	dir := t.TempDir()
	row := probeAuditLog(time.Now().UTC(), dir)
	if row.Status != "healthy" {
		t.Errorf("Status = %q; want healthy (%s)", row.Status, row.Detail)
	}
	if _, err := os.Stat(filepath.Join(dir, ".audit-health-probe")); !os.IsNotExist(err) {
		t.Errorf("probe file cleanup err=%v; want removed", err)
	}
}
