package plugins_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
	"github.com/ehmo/gum/internal/plugins/registry"
)

// TestValidateNewPluginSchemasLoadErrorWraps pins
// ValidateNewPluginSchemas's `reg.Load err → wrap` arm
// (schema_ref.go:113-115). Reached by planting a plugin-catalog.json
// with an unsupported schema_version — Load surfaces the catalog
// parse error and ValidateNewPluginSchemas wraps it with the
// "schema collision check: load registry:" prefix so installers know
// the error is registry-load (not actually a collision).
func TestValidateNewPluginSchemasLoadErrorWraps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin-catalog.json"),
		[]byte(`{"plugin_catalog_schema_version":999}`), 0o600); err != nil {
		t.Fatalf("plant catalog: %v", err)
	}
	r := registry.New(dir)
	candidate := []plugins.SchemaRef{
		{Ref: "#/types/X", Hash: "sha256:aaa", OwnerPlugin: "plugin-a"},
	}
	err := plugins.ValidateNewPluginSchemas(r, candidate)
	if err == nil {
		t.Fatal("ValidateNewPluginSchemas(bad catalog) err=nil; want wrap")
	}
	if !strings.Contains(err.Error(), "schema collision check: load registry") {
		t.Errorf("err=%q; want 'schema collision check: load registry' prefix", err.Error())
	}
}
