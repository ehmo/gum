package catalog_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
)

// TestLoadPluginCatalogJSONErrorWraps pins the json.Unmarshal err
// arm (plugin_files.go:59-61). A non-JSON payload trips the parser so
// the loader must wrap the error with the "catalog: LoadPluginCatalog:"
// prefix.
func TestLoadPluginCatalogJSONErrorWraps(t *testing.T) {
	t.Parallel()
	_, err := catalog.LoadPluginCatalog([]byte("not-json"))
	if err == nil {
		t.Fatal("LoadPluginCatalog(not-json) err=nil; want JSON wrap")
	}
	if !strings.Contains(err.Error(), "catalog: LoadPluginCatalog") {
		t.Errorf("err=%q; want 'catalog: LoadPluginCatalog' prefix", err.Error())
	}
}

// TestLoadPluginsLockJSONErrorWraps pins the analogous arm in
// LoadPluginsLock (plugin_files.go:73-75).
func TestLoadPluginsLockJSONErrorWraps(t *testing.T) {
	t.Parallel()
	_, err := catalog.LoadPluginsLock([]byte("not-json"))
	if err == nil {
		t.Fatal("LoadPluginsLock(not-json) err=nil; want JSON wrap")
	}
	if !strings.Contains(err.Error(), "catalog: LoadPluginsLock") {
		t.Errorf("err=%q; want 'catalog: LoadPluginsLock' prefix", err.Error())
	}
}

// TestLoadPluginStateJSONErrorWraps pins the analogous arm in
// LoadPluginState (plugin_files.go:87-89).
func TestLoadPluginStateJSONErrorWraps(t *testing.T) {
	t.Parallel()
	_, err := catalog.LoadPluginState([]byte("not-json"))
	if err == nil {
		t.Fatal("LoadPluginState(not-json) err=nil; want JSON wrap")
	}
	if !strings.Contains(err.Error(), "catalog: LoadPluginState") {
		t.Errorf("err=%q; want 'catalog: LoadPluginState' prefix", err.Error())
	}
}
