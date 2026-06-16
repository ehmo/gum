package mcp

import (
	"strings"
	"testing"
	"time"
)

// TestProbeGainLedgerNoHomeReturnsDegraded pins probeGainLedger's
// `os.UserHomeDir err → degraded` arm (health_probes.go:144-151). On
// Unix os.UserHomeDir returns "$HOME is not defined" when $HOME is
// empty; the probe MUST surface "home dir unresolvable: ..." rather
// than panic or report healthy, so operators can distinguish a
// fresh install (ledger absent, status=healthy) from a broken
// runtime environment.
func TestProbeGainLedgerNoHomeReturnsDegraded(t *testing.T) {
	t.Setenv("HOME", "")
	// Also blank XDG_DATA_HOME so nothing else fills in for HOME.
	t.Setenv("XDG_DATA_HOME", "")
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

	got := probeGainLedger(now, "")
	if got.Subsystem != "gain_ledger" {
		t.Errorf("Subsystem=%q; want gain_ledger", got.Subsystem)
	}
	if got.Status != "degraded" {
		t.Errorf("Status=%q; want degraded (HOME unresolvable)", got.Status)
	}
	if !strings.Contains(got.Detail, "home dir unresolvable") {
		t.Errorf("Detail=%q; want substring 'home dir unresolvable'", got.Detail)
	}
	if !got.LastCheckAt.Equal(now) {
		t.Errorf("LastCheckAt=%v; want %v", got.LastCheckAt, now)
	}
}

// TestHealthSnapshotMissingProbeReportsDegraded pins
// healthSnapshotCache.snapshot's `probe, ok := healthProbes[name];
// !ok → "probe not registered"` arm (health_probes.go:65-72). When a
// subsystem is listed in staticHealthSubsystems but no probe is
// registered (e.g., a future minor-version spec PR widens the enum
// before the runtime catches up), the snapshot MUST emit a degraded
// row rather than silently drop it — spec §13 line 3149 requires the
// row set to be complete.
func TestHealthSnapshotMissingProbeReportsDegraded(t *testing.T) {
	// Temporarily widen staticHealthSubsystems with a name that has no
	// corresponding probe in healthProbes. We restore on cleanup so
	// downstream tests see the canonical enum.
	orig := staticHealthSubsystems
	staticHealthSubsystems = append([]string(nil), orig...)
	staticHealthSubsystems = append(staticHealthSubsystems, "ghost_subsystem")
	t.Cleanup(func() { staticHealthSubsystems = orig })

	c := &healthSnapshotCache{}
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	rows := c.snapshot(now, "")

	var ghost *subsystemHealth
	for i := range rows {
		if rows[i].Subsystem == "ghost_subsystem" {
			ghost = &rows[i]
			break
		}
	}
	if ghost == nil {
		t.Fatalf("snapshot did not emit ghost_subsystem row; got %v", rows)
	}
	if ghost.Status != "degraded" {
		t.Errorf("ghost.Status=%q; want degraded", ghost.Status)
	}
	if ghost.Detail != "probe not registered" {
		t.Errorf("ghost.Detail=%q; want 'probe not registered'", ghost.Detail)
	}
	if !ghost.LastCheckAt.Equal(now) {
		t.Errorf("ghost.LastCheckAt=%v; want %v", ghost.LastCheckAt, now)
	}
}
