// Package catalog_test — RED tests for ABI version rejection (gum-5wwz).
//
// These tests exercise the ABI gating required by spec.md §5.3 and
// docs/catalog-abi.md:
//   - CATALOG_SCHEMA_UNSUPPORTED   for catalog.json schema_version mismatches
//   - PLUGIN_CATALOG_SCHEMA_UNSUPPORTED  for plugin-catalog.json
//   - PLUGIN_LOCK_SCHEMA_UNSUPPORTED     for plugins.lock
//   - PLUGIN_STATE_SCHEMA_UNSUPPORTED    for plugin-state.json
//
// All four error codes are specified in docs/catalog-abi.md § "Versioned
// Artifacts" and confirmed in spec.md error table (§12-level errors).
//
// GREEN implementation targets:
//   - catalog.Catalog.Validate() already rejects unsupported catalog_schema_version
//     via ErrUnsupportedCatalogSchemaVersion (wraps "CATALOG_SCHEMA_UNSUPPORTED").
//   - LoadPluginCatalog, LoadPluginsLock, LoadPluginState must be added (do not
//     exist yet); tests below will FAIL to compile until the green team adds them
//     to internal/catalog or internal/plugins as appropriate.
package catalog_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// ── catalog.json version gate ───────────────────────────────────────────────

// TestCatalogVersionRejectedTooNew verifies that catalog_schema_version > 1
// (the only supported version) is rejected with ErrUnsupportedCatalogSchemaVersion.
func TestCatalogVersionRejectedTooNew(t *testing.T) {
	c := validCatalogTemplate()
	c.CatalogSchemaVersion = 9999

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() returned nil; want ErrUnsupportedCatalogSchemaVersion for schema_version=9999")
	}
	if !errors.Is(err, catalog.ErrUnsupportedCatalogSchemaVersion) {
		t.Fatalf("Validate() = %v; want errors.Is(err, ErrUnsupportedCatalogSchemaVersion)", err)
	}
}

// TestCatalogVersionRejectedTooOld verifies that catalog_schema_version < 1
// (zero) is also rejected.
func TestCatalogVersionRejectedTooOld(t *testing.T) {
	c := validCatalogTemplate()
	c.CatalogSchemaVersion = 0

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() returned nil; want ErrUnsupportedCatalogSchemaVersion for schema_version=0")
	}
	if !errors.Is(err, catalog.ErrUnsupportedCatalogSchemaVersion) {
		t.Fatalf("Validate() = %v; want errors.Is(err, ErrUnsupportedCatalogSchemaVersion)", err)
	}
}

// TestCatalogVersionAccepted verifies that catalog_schema_version == 1 loads OK.
func TestCatalogVersionAccepted(t *testing.T) {
	c := validCatalogTemplate()
	c.CatalogSchemaVersion = 1 // the only currently supported version

	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() = %v; want nil for supported catalog_schema_version=1", err)
	}
}

// ── plugin-catalog.json version gate ────────────────────────────────────────

// TestPluginCatalogVersionRejected verifies that an unsupported
// plugin_catalog_schema_version returns ErrUnsupportedPluginCatalogSchemaVersion.
//
// GREEN: implement catalog.LoadPluginCatalog(data []byte) (*catalog.PluginCatalog, error).
// It must return ErrUnsupportedPluginCatalogSchemaVersion for version != 1.
func TestPluginCatalogVersionRejected(t *testing.T) {
	raw := buildPluginCatalogJSON(t, 9999)

	_, err := catalog.LoadPluginCatalog(raw)
	if err == nil {
		t.Fatal("LoadPluginCatalog returned nil error; want ErrUnsupportedPluginCatalogSchemaVersion")
	}
	if !errors.Is(err, catalog.ErrUnsupportedPluginCatalogSchemaVersion) {
		t.Fatalf("LoadPluginCatalog = %v; want errors.Is(err, ErrUnsupportedPluginCatalogSchemaVersion)", err)
	}
}

// TestPluginCatalogVersionAccepted verifies that plugin_catalog_schema_version=1 is accepted.
func TestPluginCatalogVersionAccepted(t *testing.T) {
	raw := buildPluginCatalogJSON(t, 1)

	_, err := catalog.LoadPluginCatalog(raw)
	if err != nil {
		t.Fatalf("LoadPluginCatalog returned %v; want nil for supported version=1", err)
	}
}

// ── plugins.lock version gate ────────────────────────────────────────────────

// TestPluginsLockVersionRejected verifies that an unsupported plugins_lock_schema_version
// returns ErrUnsupportedPluginsLockSchemaVersion.
//
// GREEN: implement catalog.LoadPluginsLock(data []byte) (*catalog.PluginsLock, error).
func TestPluginsLockVersionRejected(t *testing.T) {
	raw := buildPluginsLockJSON(t, 9999)

	_, err := catalog.LoadPluginsLock(raw)
	if err == nil {
		t.Fatal("LoadPluginsLock returned nil error; want ErrUnsupportedPluginsLockSchemaVersion")
	}
	if !errors.Is(err, catalog.ErrUnsupportedPluginsLockSchemaVersion) {
		t.Fatalf("LoadPluginsLock = %v; want errors.Is(err, ErrUnsupportedPluginsLockSchemaVersion)", err)
	}
}

// TestPluginsLockVersionAccepted verifies that plugins_lock_schema_version=1 is accepted.
func TestPluginsLockVersionAccepted(t *testing.T) {
	raw := buildPluginsLockJSON(t, 1)

	_, err := catalog.LoadPluginsLock(raw)
	if err != nil {
		t.Fatalf("LoadPluginsLock returned %v; want nil for supported version=1", err)
	}
}

// ── plugin-state.json version gate ───────────────────────────────────────────

// TestPluginStateVersionRejected verifies that an unsupported
// plugin_state_schema_version returns ErrUnsupportedPluginStateSchemaVersion.
//
// GREEN: implement catalog.LoadPluginState(data []byte) (*catalog.PluginState, error).
func TestPluginStateVersionRejected(t *testing.T) {
	raw := buildPluginStateJSON(t, 9999)

	_, err := catalog.LoadPluginState(raw)
	if err == nil {
		t.Fatal("LoadPluginState returned nil error; want ErrUnsupportedPluginStateSchemaVersion")
	}
	if !errors.Is(err, catalog.ErrUnsupportedPluginStateSchemaVersion) {
		t.Fatalf("LoadPluginState = %v; want errors.Is(err, ErrUnsupportedPluginStateSchemaVersion)", err)
	}
}

// TestPluginStateVersionAccepted verifies that plugin_state_schema_version=1 is accepted.
func TestPluginStateVersionAccepted(t *testing.T) {
	raw := buildPluginStateJSON(t, 1)

	_, err := catalog.LoadPluginState(raw)
	if err != nil {
		t.Fatalf("LoadPluginState returned %v; want nil for supported version=1", err)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// validCatalogTemplate returns a minimal valid Catalog (schema_version=1).
func validCatalogTemplate() *catalog.Catalog {
	return &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-05-23T00:00:00Z",
		GeneratorVersion:     "gum/cmd/gen-catalog@test",
		Ops:                  nil,
	}
}

func buildPluginCatalogJSON(t *testing.T, version int) []byte {
	t.Helper()
	obj := map[string]any{
		"plugin_catalog_schema_version": version,
		"updated_at":                    "2026-05-23T00:00:00Z",
		"variants":                      []any{},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("buildPluginCatalogJSON: %v", err)
	}
	return b
}

func buildPluginsLockJSON(t *testing.T, version int) []byte {
	t.Helper()
	obj := map[string]any{
		"plugins_lock_schema_version": version,
		"install_generation":          1,
		"install_txid":                "abc123",
		"plugins":                     []any{},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("buildPluginsLockJSON: %v", err)
	}
	return b
}

func buildPluginStateJSON(t *testing.T, version int) []byte {
	t.Helper()
	obj := map[string]any{
		"plugin_state_schema_version": version,
		"install_generation":          1,
		"install_txid":                "abc123",
		"plugins":                     []any{},
	}
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("buildPluginStateJSON: %v", err)
	}
	return b
}
