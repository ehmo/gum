package registry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestLoadCatalogParseErrorPropagates pins the LoadPluginCatalog error
// arm: an unsupported plugin_catalog_schema_version is the canonical
// "future schema" failure shape. Load MUST surface
// ErrUnsupportedPluginCatalogSchemaVersion via errors.Is so callers can
// branch on it (vs. swallowing as empty catalog).
func TestLoadCatalogParseErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, CatalogFilename),
		[]byte(`{"plugin_catalog_schema_version":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(dir)
	_, err := r.Load()
	if !errors.Is(err, catalog.ErrUnsupportedPluginCatalogSchemaVersion) {
		t.Errorf("err=%v; want ErrUnsupportedPluginCatalogSchemaVersion wrap", err)
	}
}

// TestLoadLockParseErrorPropagates pins the LoadPluginsLock error arm:
// must surface ErrUnsupportedPluginsLockSchemaVersion for a future
// lock schema rather than silently fall through to empty.
func TestLoadLockParseErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, LockFilename),
		[]byte(`{"plugins_lock_schema_version":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(dir)
	_, err := r.Load()
	if !errors.Is(err, catalog.ErrUnsupportedPluginsLockSchemaVersion) {
		t.Errorf("err=%v; want ErrUnsupportedPluginsLockSchemaVersion wrap", err)
	}
}

// TestLoadStateParseErrorPropagates pins the LoadPluginState error arm:
// must surface ErrUnsupportedPluginStateSchemaVersion for a future
// state schema rather than silently fall through to empty.
func TestLoadStateParseErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, StateFilename),
		[]byte(`{"plugin_state_schema_version":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(dir)
	_, err := r.Load()
	if !errors.Is(err, catalog.ErrUnsupportedPluginStateSchemaVersion) {
		t.Errorf("err=%v; want ErrUnsupportedPluginStateSchemaVersion wrap", err)
	}
}

// TestLoadReadErrorPropagates pins the readIfExists error arm: when one
// of the three paths is a directory instead of a regular file, the
// underlying os.ReadFile returns EISDIR which propagates through Load.
func TestLoadReadErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	// Plant a directory where the catalog file should live.
	if err := os.Mkdir(filepath.Join(dir, CatalogFilename), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(dir)
	_, err := r.Load()
	if err == nil {
		t.Fatal("want read error; got nil")
	}
}
