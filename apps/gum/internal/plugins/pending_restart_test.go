package plugins_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestPluginInactiveInventoryOnly is the bead-named acceptance for gum-dlf:
// a plugin marked installed_pending_restart MUST appear in the inventory
// list (so the CLI can show "installed, restart required") but MUST NOT
// appear in the active list used for dispatch + MCP completions.
func TestPluginInactiveInventoryOnly(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	ctx := context.Background()
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	if err := plugins.MarkInstalledPendingRestart(ctx, reg, "flights", now); err != nil {
		t.Fatalf("MarkInstalledPendingRestart: %v", err)
	}

	inv, err := plugins.InventoryPluginNames(reg)
	if err != nil {
		t.Fatalf("InventoryPluginNames: %v", err)
	}
	if !contains(inv, "flights") {
		t.Errorf("inventory = %v; want to contain flights", inv)
	}

	active, err := plugins.ActivePluginNames(reg)
	if err != nil {
		t.Fatalf("ActivePluginNames: %v", err)
	}
	if contains(active, "flights") {
		t.Errorf("active = %v; want flights ABSENT (pending restart)", active)
	}
}

// TestPendingRestartExcludedFromCompletions pins the spec §13 line 3148 rule
// at the helper level used by gum.search_apis: an installed_pending_restart
// plugin MUST be invisible to any caller of ActivePluginNames, even when
// the same plugin's row carries other state-fields like quarantined=false.
func TestPendingRestartExcludedFromCompletions(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed a steady-state active plugin and a freshly installed one.
	if err := plugins.MarkInstalledPendingRestart(ctx, reg, "fresh", now); err != nil {
		t.Fatalf("seed fresh: %v", err)
	}
	if err := plugins.MarkInstalledPendingRestart(ctx, reg, "settled", now.Add(-time.Hour)); err != nil {
		t.Fatalf("seed settled: %v", err)
	}
	if _, err := plugins.PromotePendingRestart(ctx, reg, now); err != nil {
		t.Fatalf("PromotePendingRestart: %v", err)
	}
	// Re-install "fresh" after the promotion: it must drop back to pending.
	if err := plugins.MarkInstalledPendingRestart(ctx, reg, "fresh", now); err != nil {
		t.Fatalf("re-seed fresh: %v", err)
	}

	active, err := plugins.ActivePluginNames(reg)
	if err != nil {
		t.Fatalf("ActivePluginNames: %v", err)
	}
	sort.Strings(active)
	want := []string{"settled"}
	if !equal(active, want) {
		t.Errorf("active = %v; want %v (fresh re-install must be excluded)", active, want)
	}

	inv, err := plugins.InventoryPluginNames(reg)
	if err != nil {
		t.Fatalf("InventoryPluginNames: %v", err)
	}
	sort.Strings(inv)
	wantInv := []string{"fresh", "settled"}
	if !equal(inv, wantInv) {
		t.Errorf("inventory = %v; want %v (both rows visible to operator)", inv, wantInv)
	}
}

// TestPromotePendingRestartReportsPromotedNames asserts the bootstrap-time
// promotion returns the list of newly promoted plugins so the boot path
// can log a structured event per row.
func TestPromotePendingRestartReportsPromotedNames(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	ctx := context.Background()
	now := time.Now().UTC()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := plugins.MarkInstalledPendingRestart(ctx, reg, name, now); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	promoted, err := plugins.PromotePendingRestart(ctx, reg, now)
	if err != nil {
		t.Fatalf("PromotePendingRestart: %v", err)
	}
	sort.Strings(promoted)
	want := []string{"alpha", "beta", "gamma"}
	if !equal(promoted, want) {
		t.Errorf("promoted = %v; want %v", promoted, want)
	}

	// A second call is a no-op — already-active rows are not re-promoted.
	again, err := plugins.PromotePendingRestart(ctx, reg, now)
	if err != nil {
		t.Fatalf("PromotePendingRestart (idempotent): %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second promote returned %v; want empty (idempotent)", again)
	}

	active, err := plugins.ActivePluginNames(reg)
	if err != nil {
		t.Fatalf("ActivePluginNames: %v", err)
	}
	sort.Strings(active)
	if !equal(active, want) {
		t.Errorf("active = %v; want %v after promotion", active, want)
	}
}

// TestMarkPendingRestartPreservesQuarantineState pins the cross-cutting
// invariant from supervisor.go: marking a plugin pending MUST NOT clear
// supervisor fields like quarantined / retry_count / backoff_step. Otherwise
// a malicious re-install could be used to bypass quarantine.
func TestMarkPendingRestartPreservesQuarantineState(t *testing.T) {
	defer goleak.VerifyNone(t)

	reg := registry.New(t.TempDir())
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := plugins.RecordCrash(ctx, reg, "flights", "SERVICE_DOWN", now); err != nil {
		t.Fatalf("RecordCrash: %v", err)
	}
	if err := plugins.MarkInstalledPendingRestart(ctx, reg, "flights", now); err != nil {
		t.Fatalf("MarkInstalledPendingRestart: %v", err)
	}
	state, err := plugins.ReadSupervisorState(reg, "flights")
	if err != nil {
		t.Fatalf("ReadSupervisorState: %v", err)
	}
	if !state.Quarantined {
		t.Errorf("state.Quarantined = false after re-install; want true (re-install MUST NOT clear quarantine)")
	}
	if state.BackoffStep != 1 {
		t.Errorf("state.BackoffStep = %d; want 1 (preserved)", state.BackoffStep)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
