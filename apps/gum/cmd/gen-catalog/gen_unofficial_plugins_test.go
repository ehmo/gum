package main

import (
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestUnofficialPluginsOpShape — gum-76q acceptance. The four spec §1.1
// "defensible-surface unofficial APIs" (Scholar, Patents, YouTube Transcripts,
// Trends) each surface as a single Shape 1 plugin op. This test pins the
// canonical (op_id, default_variant_id, plugin_name, tool_name) tuple so the
// host plugin dispatcher and gum.search_apis caller agree on the same surface.
func TestUnofficialPluginsOpShape(t *testing.T) {
	ops := BuildUnofficialPluginOps()
	if got, want := len(ops), 4; got != want {
		t.Fatalf("BuildUnofficialPluginOps returned %d ops; want %d (Scholar, Patents, YouTube Transcripts, Trends)", got, want)
	}

	cases := []struct {
		opID       string
		variantID  string
		pluginName string
		toolName   string
		service    string
	}{
		{"scholar.search", "scholar.v1.plugin.search", "google-scholar", "scholar_search", "scholar"},
		{"patents.search", "patents.v1.plugin.search", "google-patents", "patents_search", "patents"},
		{"youtube.transcripts.get", "youtube.transcripts.v1.plugin.get", "youtube-transcripts", "youtube_transcripts_get", "youtube"},
		{"trends.daily", "trends.v1.plugin.daily", "google-trends", "trends_daily", "trends"},
	}

	for _, c := range cases {
		t.Run(c.opID, func(t *testing.T) {
			var op catalog.Op
			for _, candidate := range ops {
				if candidate.OpID == c.opID {
					op = candidate
					break
				}
			}
			if op.OpID == "" {
				t.Fatalf("op_id %q not found in BuildUnofficialPluginOps result", c.opID)
			}
			if op.OpSchemaVersion != 1 {
				t.Errorf("op_schema_version = %d; want 1", op.OpSchemaVersion)
			}
			if op.ServiceFamily != "plugin" {
				t.Errorf("service_family = %q; want plugin (spec §1.1: unofficial-API plugins)", op.ServiceFamily)
			}
			if op.Service != c.service {
				t.Errorf("service = %q; want %q", op.Service, c.service)
			}
			if op.DefaultVariantID != c.variantID {
				t.Errorf("default_variant_id = %q; want %q", op.DefaultVariantID, c.variantID)
			}
			if got := len(op.Variants); got != 1 {
				t.Fatalf("variants = %d; want 1 (single-variant default per Shape 1)", got)
			}
			v := op.Variants[0]
			if v.InterfaceKind != catalog.InterfaceKindPluginMCP {
				t.Errorf("interface_kind = %q; want plugin-mcp (spec §8.2)", v.InterfaceKind)
			}
			if v.BackendKind != catalog.BackendKindMCPPlugin {
				t.Errorf("backend_kind = %q; want mcp-plugin (spec §8.2)", v.BackendKind)
			}
			if v.RiskClass != catalog.RiskClassRead {
				t.Errorf("risk_class = %q; want read (Scholar/Patents/Trends/Transcripts are query-only)", v.RiskClass)
			}
			if v.AuthStrategy != catalog.AuthStrategyPluginManaged {
				t.Errorf("auth_strategy = %q; want plugin_managed (unofficial plugins own their own credentials)", v.AuthStrategy)
			}
			if !v.Preferred {
				t.Error("preferred = false; want true (single-variant default)")
			}
			if v.Binding == nil {
				t.Fatal("binding is nil; plugin variants need Binding to resolve adapter_key + tool_name")
			}
			if v.Binding.AdapterKey != "plugin.mcp" {
				t.Errorf("binding.adapter_key = %q; want plugin.mcp", v.Binding.AdapterKey)
			}
			if v.Binding.PluginName != c.pluginName {
				t.Errorf("binding.plugin_name = %q; want %q (matches apps/gum/plugins/%s/manifest.json plugin_id)",
					v.Binding.PluginName, c.pluginName, c.pluginName)
			}
			if v.Binding.ToolName != c.toolName {
				t.Errorf("binding.tool_name = %q; want %q (matches manifest advertised_tools[].name)",
					v.Binding.ToolName, c.toolName)
			}
			if v.Binding.OperationKey != c.opID {
				t.Errorf("binding.operation_key = %q; want %q", v.Binding.OperationKey, c.opID)
			}
		})
	}
}

// TestUnofficialPluginsOpsValidate builds a catalog containing only the four
// unofficial-API plugin ops and asserts it passes catalog.Validate so
// generator-side schema checks won't reject the variants once they are
// appended to the embedded snapshot.
func TestUnofficialPluginsOpsValidate(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  BuildUnofficialPluginOps(),
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("validate unofficial-plugins-only catalog: %v", err)
	}
}

// TestUnofficialPluginsBindingPluginShape — the mcp-plugin executor at install
// time validates that binding.tool_name is set for backend_kind=mcp-plugin
// (spec §8.2 line 1593). Failing this gate at gen-catalog time means the
// install would later refuse the variant. Pin it here so manifest drift is
// caught build-side rather than user-side.
func TestUnofficialPluginsBindingPluginShape(t *testing.T) {
	for _, op := range BuildUnofficialPluginOps() {
		t.Run(op.OpID, func(t *testing.T) {
			v := op.Variants[0]
			if v.BackendKind != catalog.BackendKindMCPPlugin {
				t.Fatalf("backend_kind = %q; want mcp-plugin (precondition)", v.BackendKind)
			}
			if v.Binding == nil {
				t.Fatal("binding is nil")
			}
			if v.Binding.ToolName == "" {
				t.Error("binding.tool_name is empty; spec §8.2 requires tool_name for mcp-plugin")
			}
			if v.Binding.PluginName == "" {
				t.Error("binding.plugin_name is empty; spec §8.2 requires plugin_name for mcp-plugin")
			}
		})
	}
}
