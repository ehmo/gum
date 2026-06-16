package plugins_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// FuzzPluginManifest writes arbitrary bytes to a temp manifest.json and calls
// LoadManifest. A "pass" is any panic-free return — typed errors are
// acceptable. Required by spec §15 release-pipeline step 2.
func FuzzPluginManifest(f *testing.F) {
	seeds := [][]byte{
		// Empty / minimal
		[]byte(``),
		[]byte(`{}`),
		// Valid-ish manifest skeleton
		[]byte(`{"manifest_schema_version":1,"shape":"mcp-plugin","plugin_id":"foo.bar","executable":"./run"}`),
		// Wrong schema version
		[]byte(`{"manifest_schema_version":999,"shape":"mcp-plugin","plugin_id":"foo","executable":"./run"}`),
		// Wrong shape
		[]byte(`{"manifest_schema_version":1,"shape":"native-bin","plugin_id":"foo","executable":"./run"}`),
		// Missing executable
		[]byte(`{"manifest_schema_version":1,"shape":"mcp-plugin","plugin_id":"foo","executable":""}`),
		// Bad plugin_id
		[]byte(`{"manifest_schema_version":1,"shape":"mcp-plugin","plugin_id":"123","executable":"./run"}`),
		// Advertised tools
		[]byte(`{"manifest_schema_version":1,"shape":"mcp-plugin","plugin_id":"foo.bar","executable":"./run","advertised_tools":[{"name":"t","risk_class":"read"}]}`),
		// Bad risk class
		[]byte(`{"manifest_schema_version":1,"shape":"mcp-plugin","plugin_id":"foo.bar","executable":"./run","advertised_tools":[{"name":"t","risk_class":"explode"}]}`),
		// Malformed JSON
		[]byte(`{`),
		[]byte(`{"a":`),
		// Huge nested
		[]byte(`{"a":` + `{"b":` + `{"c":1}}}`),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		_, _ = plugins.LoadManifest(dir)
	})
}
