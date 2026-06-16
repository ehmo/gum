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

// plantStateWithNonMapRow plants a plugin-state.json with a non-map row
// followed by a valid plugin row. Returns the registry rooted at the
// plant dir. Used to exercise the inner-loop `!ok → continue` arms in
// ActivePluginNames, InventoryPluginNames, and PromotePendingRestart.
func plantStateWithNonMapRow(t *testing.T, validName string) *registry.Registry {
	t.Helper()
	dir := t.TempDir()
	state := `{
		"plugin_state_schema_version": 1,
		"install_generation": 1,
		"install_txid": "tx1",
		"plugins": [
			"not-a-map",
			{"name": "` + validName + `", "status": "active"}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(state), 0o600); err != nil {
		t.Fatalf("plant state: %v", err)
	}
	return registry.New(dir)
}

// TestActivePluginNamesLoadErrorPropagates pins ActivePluginNames's
// `reg.Load err → return err` arm (pending_restart.go:91-93). Reached
// by planting a future-schema plugin-catalog.json — Load surfaces the
// version-bump error and ActivePluginNames returns it verbatim.
func TestActivePluginNamesLoadErrorPropagates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin-catalog.json"),
		[]byte(`{"plugin_catalog_schema_version":999}`), 0o600); err != nil {
		t.Fatalf("plant catalog: %v", err)
	}
	reg := registry.New(dir)
	_, err := plugins.ActivePluginNames(reg)
	if err == nil {
		t.Fatal("ActivePluginNames(bad catalog) err=nil; want load err")
	}
}

// TestInventoryPluginNamesLoadErrorPropagates pins the analogous arm
// in InventoryPluginNames (pending_restart.go:117-119).
func TestInventoryPluginNamesLoadErrorPropagates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin-catalog.json"),
		[]byte(`{"plugin_catalog_schema_version":999}`), 0o600); err != nil {
		t.Fatalf("plant catalog: %v", err)
	}
	reg := registry.New(dir)
	_, err := plugins.InventoryPluginNames(reg)
	if err == nil {
		t.Fatal("InventoryPluginNames(bad catalog) err=nil; want load err")
	}
}

// TestActivePluginNamesSkipsNonMapRows pins ActivePluginNames's
// `!ok → continue` arm (pending_restart.go:97-98). A non-map row must
// be skipped, not panic, and the valid row must still surface.
func TestActivePluginNamesSkipsNonMapRows(t *testing.T) {
	t.Parallel()
	reg := plantStateWithNonMapRow(t, "valid-plugin")
	got, err := plugins.ActivePluginNames(reg)
	if err != nil {
		t.Fatalf("ActivePluginNames: %v", err)
	}
	if len(got) != 1 || got[0] != "valid-plugin" {
		t.Errorf("got=%v; want [valid-plugin]", got)
	}
}

// TestInventoryPluginNamesSkipsNonMapRows pins the analogous arm in
// InventoryPluginNames (pending_restart.go:123-124).
func TestInventoryPluginNamesSkipsNonMapRows(t *testing.T) {
	t.Parallel()
	reg := plantStateWithNonMapRow(t, "valid-plugin")
	got, err := plugins.InventoryPluginNames(reg)
	if err != nil {
		t.Fatalf("InventoryPluginNames: %v", err)
	}
	if len(got) != 1 || got[0] != "valid-plugin" {
		t.Errorf("got=%v; want [valid-plugin]", got)
	}
}

// TestPromotePendingRestartSkipsNonMapRows pins PromotePendingRestart's
// `!ok → continue` arm (pending_restart.go:60-61). The non-map row is
// skipped without affecting the valid pending row's promotion.
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
