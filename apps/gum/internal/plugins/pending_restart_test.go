package plugins_test

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// seedPendingRows writes one installed_pending_restart row per name, the
// shape install_registry.go's initialPluginState produces. Tests that need
// the install path itself use installOne in install_initial_state_test.go;
// this is for the promotion cases that only need N rows.
func seedPendingRows(t *testing.T, reg *registry.Registry, names ...string) {
	t.Helper()
	err := reg.WriteTransaction(context.Background(), func(f *registry.Files) error {
		for _, name := range names {
			f.State.Plugins = append(f.State.Plugins, map[string]any{
				"name":         name,
				"status":       plugins.StatusInstalledPendingRestart,
				"installed_at": time.Now().UTC().Format(time.RFC3339),
				"activated_at": nil,
				"quarantined":  false,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed pending rows: %v", err)
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

	seedPendingRows(t, reg, "alpha", "beta", "gamma")

	promoted, err := plugins.PromotePendingRestart(ctx, reg, now)
	if err != nil {
		t.Fatalf("PromotePendingRestart: %v", err)
	}
	sort.Strings(promoted)
	want := []string{"alpha", "beta", "gamma"}
	if !slices.Equal(promoted, want) {
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

	rows, err := plugins.InventoryRows(reg)
	if err != nil {
		t.Fatalf("InventoryRows: %v", err)
	}
	for _, row := range rows {
		if row.Status != plugins.StatusActive {
			t.Errorf("%s status = %q after promotion; want %q", row.Name, row.Status, plugins.StatusActive)
		}
	}
}
