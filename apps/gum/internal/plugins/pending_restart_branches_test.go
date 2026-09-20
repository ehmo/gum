package plugins_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestPromotePendingRestartLoadErrorPropagates pins the load-error arm of
// hasPendingRestart, the cheap pre-check PromotePendingRestart runs before it
// takes the install lock. Reached by planting a future-schema
// plugin-catalog.json: Load surfaces the version-bump error, and promotion
// must return it rather than read the profile as "nothing pending".
func TestPromotePendingRestartLoadErrorPropagates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin-catalog.json"),
		[]byte(`{"plugin_catalog_schema_version":999}`), 0o600); err != nil {
		t.Fatalf("plant catalog: %v", err)
	}
	_, err := plugins.PromotePendingRestart(context.Background(), registry.New(dir), time.Now())
	if err == nil {
		t.Fatal("PromotePendingRestart(bad catalog) err=nil; want load err")
	}
}

// TestPromotePendingRestartSkipsNonMapRows pins PromotePendingRestart's
// `!ok → continue` arm. The non-map row is skipped without affecting the
// valid pending row's promotion.
func TestPromotePendingRestartSkipsNonMapRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	state := `{
		"plugin_state_schema_version": 1,
		"install_generation": 1,
		"install_txid": "tx1",
		"plugins": [
			"not-a-map",
			{"name": "pending-plugin", "status": "installed_pending_restart"}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(state), 0o600); err != nil {
		t.Fatalf("plant state: %v", err)
	}
	reg := registry.New(dir)
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	promoted, err := plugins.PromotePendingRestart(context.Background(), reg, now)
	if err != nil {
		t.Fatalf("PromotePendingRestart: %v", err)
	}
	if len(promoted) != 1 || promoted[0] != "pending-plugin" {
		t.Errorf("promoted=%v; want [pending-plugin]", promoted)
	}
}
