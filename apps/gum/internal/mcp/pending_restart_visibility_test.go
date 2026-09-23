package mcp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// seedPluginStatuses points XDG_DATA_HOME at a fresh temp dir, writes one
// plugin-state.json row per name/status pair, and returns the profile dir so
// the caller can read the same file through the CLI inventory path.
//
// plugins.lock stays empty on purpose: loadPluginInventoryRows folds lock
// metadata in by name, and these tests only assert which names survive the
// §13 status filter.
func seedPluginStatuses(t *testing.T, statuses map[string]string) string {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	profileDir := filepath.Join(dataHome, "gum", "default")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", profileDir, err)
	}
	rows := make([]map[string]any, 0, len(statuses))
	for name, status := range statuses {
		rows = append(rows, map[string]any{"name": name, "status": status})
	}
	writePluginFiles(t, profileDir,
		map[string]any{"plugin_state_schema_version": 1, "plugins": rows},
		map[string]any{"plugins_lock_schema_version": 1, "plugins": []map[string]any{}},
	)
	return profileDir
}

// TestPluginInactiveInventoryOnly is the two-sided acceptance for the
// installed_pending_restart status: the operator inventory keeps the row so
// `gum plugin list` can report "installed, restart required", and every MCP
// surface drops it so nothing can invoke a plugin this process never loaded
// (spec §8.7 + §13).
//
// Both halves run against production readers: plugins.InventoryRows is what
// cmd/gum/plugin.go lists, and Server.loadPluginInventoryRows is what every
// MCP plugin surface reads.
func TestPluginInactiveInventoryOnly(t *testing.T) {
	profileDir := seedPluginStatuses(t, map[string]string{
		"fresh":   "installed_pending_restart",
		"settled": "active",
	})

	rows, err := plugins.InventoryRows(registry.New(profileDir))
	if err != nil {
		t.Fatalf("InventoryRows: %v", err)
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	if want := []string{"fresh", "settled"}; !slices.Equal(names, want) {
		t.Errorf("operator inventory = %v; want %v (a pending row stays visible)", names, want)
	}

	s := &Server{}
	res, err := s.handlePluginsRead(context.Background(), newPluginsReadReq("gum://plugins"))
	if err != nil {
		t.Fatalf("handlePluginsRead: %v", err)
	}
	body := res.Contents[0].Text
	if strings.Contains(body, "fresh") {
		t.Errorf("gum://plugins lists the pending-restart plugin:\n%s", body)
	}
	if !strings.Contains(body, "settled") {
		t.Errorf("gum://plugins dropped the active plugin:\n%s", body)
	}
}

// TestPendingRestartExcludedFromCompletions pins the completion half of the
// same rule. Server.completionPluginNames feeds `completion/complete`, and it
// reads the filtered loader, so an installed_pending_restart plugin can never
// be completed. A needs_configuration plugin still can: spec §13
// permits inventory-only names because gum://plugin/{name} is metadata-only.
func TestPendingRestartExcludedFromCompletions(t *testing.T) {
	seedPluginStatuses(t, map[string]string{
		"alpha-active": "active",
		"beta-pending": "installed_pending_restart",
		"gamma-config": "needs_configuration",
	})

	s := &Server{}
	got := s.completionPluginNames()
	want := []string{"alpha-active", "gamma-config"}
	if !slices.Equal(got, want) {
		t.Errorf("completion plugin names = %v; want %v (pending-restart excluded)", got, want)
	}
}
