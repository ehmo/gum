package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// quarantinedState is the gum-mmzr repro state file, verbatim in shape: two
// plugins quarantined with PLUGIN_MANIFEST_NOT_FOUND. Neither has a loadable
// manifest, so a manifest-only listing shows nothing at all.
const quarantinedState = `{
  "plugin_state_schema_version": 1,
  "install_generation": 4,
  "install_txid": "76e15842",
  "plugins": [
    {
      "name": "google-trends",
      "quarantined": true,
      "permanent_quarantine": false,
      "last_error_code": "PLUGIN_MANIFEST_NOT_FOUND",
      "quarantined_at": "2026-08-06T05:23:25Z",
      "next_retry_at": "2026-08-06T05:23:55Z",
      "retry_count": 1,
      "backoff_step": 1
    },
    {
      "name": "google-scholar",
      "quarantined": true,
      "permanent_quarantine": false,
      "last_error_code": "PLUGIN_MANIFEST_NOT_FOUND",
      "quarantined_at": "2026-08-02T06:19:21Z",
      "next_retry_at": "2026-08-02T06:19:51Z",
      "retry_count": 1,
      "backoff_step": 1
    }
  ]
}`

func plantState(t *testing.T, body string) *registry.Registry {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin-state.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("plant state: %v", err)
	}
	return registry.New(dir)
}

// TestInventoryRowsReportsQuarantine is the data half of gum-mmzr: the state
// file is the only source that knows a plugin is quarantined, and every field
// the operator needs to act has to survive the read.
func TestInventoryRowsReportsQuarantine(t *testing.T) {
	t.Parallel()
	rows, err := plugins.InventoryRows(plantState(t, quarantinedState))
	if err != nil {
		t.Fatalf("InventoryRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d; want 2", len(rows))
	}
	// Sorted by name, so google-scholar comes first regardless of file order.
	if rows[0].Name != "google-scholar" || rows[1].Name != "google-trends" {
		t.Fatalf("rows = %q, %q; want google-scholar, google-trends", rows[0].Name, rows[1].Name)
	}
	got := rows[0].State
	if !got.Quarantined {
		t.Error("Quarantined = false; want true")
	}
	if got.Permanent {
		t.Error("Permanent = true; want false")
	}
	if got.LastErrorCode != "PLUGIN_MANIFEST_NOT_FOUND" {
		t.Errorf("LastErrorCode = %q; want PLUGIN_MANIFEST_NOT_FOUND", got.LastErrorCode)
	}
	if got.RetryCount != 1 || got.BackoffStep != 1 {
		t.Errorf("RetryCount/BackoffStep = %d/%d; want 1/1", got.RetryCount, got.BackoffStep)
	}
	if got.NextRetryAt.UTC().Format("2006-01-02T15:04:05Z") != "2026-08-02T06:19:51Z" {
		t.Errorf("NextRetryAt = %v; want 2026-08-02T06:19:51Z", got.NextRetryAt)
	}
}

// TestInventoryRowsCarriesStatus: the §8.7 lifecycle status rides along, so the
// listing can tell a pending-restart install from an active plugin.
func TestInventoryRowsCarriesStatus(t *testing.T) {
	t.Parallel()
	rows, err := plugins.InventoryRows(plantState(t, `{
		"plugin_state_schema_version": 1,
		"plugins": [
			{"name": "fresh", "status": "installed_pending_restart"},
			{"name": "old", "status": "active"}
		]
	}`))
	if err != nil {
		t.Fatalf("InventoryRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d; want 2", len(rows))
	}
	if rows[0].Status != plugins.StatusInstalledPendingRestart {
		t.Errorf("fresh status = %q; want %q", rows[0].Status, plugins.StatusInstalledPendingRestart)
	}
	if rows[1].Status != plugins.StatusActive {
		t.Errorf("old status = %q; want %q", rows[1].Status, plugins.StatusActive)
	}
}

// TestInventoryRowsSkipsMalformedRows: one bad row must not hide the rows
// around it — a listing that silently drops entries is the defect gum-mmzr
// reported in the first place.
func TestInventoryRowsSkipsMalformedRows(t *testing.T) {
	t.Parallel()
	rows, err := plugins.InventoryRows(plantState(t, `{
		"plugin_state_schema_version": 1,
		"plugins": ["not-a-map", {"status": "active"}, {"name": "real", "status": "active"}]
	}`))
	if err != nil {
		t.Fatalf("InventoryRows: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "real" {
		t.Fatalf("rows = %+v; want the one named row", rows)
	}
}

// TestInventoryRowsPropagatesLoadError: an unreadable registry must not look
// like an empty one.
func TestInventoryRowsPropagatesLoadError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin-catalog.json"),
		[]byte(`{"plugin_catalog_schema_version":999}`), 0o600); err != nil {
		t.Fatalf("plant catalog: %v", err)
	}
	if _, err := plugins.InventoryRows(registry.New(dir)); err == nil {
		t.Fatal("InventoryRows(bad catalog) err = nil; want the load error")
	}
}
