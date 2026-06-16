package plugins_test

import (
	"path/filepath"
	"testing"

	"github.com/ehmo/gum/internal/plugins"
)

// TestBundledManifestsAreValid — gum-76q acceptance gate. The bundled
// unofficial-API plugins from spec §1.1 (Scholar, Patents, YouTube
// Transcripts, Trends) plus the Tier A Flights plugin from §4.1 (gum-ikg)
// ship as bundled manifests under apps/gum/plugins/. This test loads each
// manifest via plugins.LoadManifest (the same code path the install/start
// gates use) and asserts:
//
//	1) the manifest passes schema validation (plugin_id, shape, executable,
//	   advertised_tools.risk_class all present and well-formed),
//	2) the plugin_id matches the directory name (so `gum plugin install
//	   apps/gum/plugins/<dir>` resolves to the same id the catalog binding
//	   references),
//	3) the advertised tool names match the catalog binding.tool_name set in
//	   cmd/gen-catalog/gen_unofficial_plugins.go.
//
// A break in any of these is the same failure shape a real user would hit at
// `gum plugin install` time. Catching it here keeps the bundled manifests
// honest against the catalog.
func TestBundledManifestsAreValid(t *testing.T) {
	bundleRoot := filepath.Join("..", "..", "plugins")

	cases := []struct {
		dir      string
		pluginID string
		tool     string
	}{
		{"google-scholar", "google-scholar", "scholar_search"},
		{"google-patents", "google-patents", "patents_search"},
		{"youtube-transcripts", "youtube-transcripts", "youtube_transcripts_get"},
		{"google-trends", "google-trends", "trends_daily"},
		{"google-flights", "google-flights", "flights_search"},
	}

	for _, c := range cases {
		t.Run(c.dir, func(t *testing.T) {
			dir := filepath.Join(bundleRoot, c.dir)
			m, err := plugins.LoadManifest(dir)
			if err != nil {
				t.Fatalf("LoadManifest(%s): %v", dir, err)
			}
			if m.PluginID != c.pluginID {
				t.Errorf("plugin_id = %q; want %q (must match directory name so install paths resolve consistently)", m.PluginID, c.pluginID)
			}
			if m.Shape != "mcp-plugin" {
				t.Errorf("shape = %q; want mcp-plugin (Shape 1 is the only supported plugin shape in v0.1.0)", m.Shape)
			}
			if m.Executable == "" {
				t.Error("executable is empty; LoadManifest should have rejected this but defense-in-depth")
			}
			// Find the tool by name.
			var found bool
			for _, tool := range m.AdvertisedTools {
				if tool.Name == c.tool {
					found = true
					if tool.RiskClass != "read" {
						t.Errorf("advertised_tools[%s].risk_class = %q; want read (unofficial-API plugins are read-only in v0.1.0)", c.tool, tool.RiskClass)
					}
					break
				}
			}
			if !found {
				t.Errorf("advertised_tools does not contain %q; catalog binding.tool_name would dispatch to a missing tool", c.tool)
			}
		})
	}
}
