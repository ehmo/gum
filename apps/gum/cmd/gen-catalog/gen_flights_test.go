package main

import (
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
)

// TestBuildFlightsOpShape — gum-9vuq.10 acceptance. The fli Shape 1 plugin
// variant must match spec §4.1 line 366 and §8.2 exactly so the convenience
// handler (internal/mcp/tier_a_abi.go) can resolve flights_search →
// flights.search → flights.v1.plugin.search and route to the mcp-plugin
// executor with the right tool_name.
func TestBuildFlightsOpShape(t *testing.T) {
	op := BuildFlightsOp()

	if op.OpID != "flights.search" {
		t.Errorf("op_id = %q; want flights.search (spec §4.1 line 366)", op.OpID)
	}
	if op.OpSchemaVersion != 1 {
		t.Errorf("op_schema_version = %d; want 1", op.OpSchemaVersion)
	}
	if op.DefaultVariantID != "flights.v1.plugin.search" {
		t.Errorf("default_variant_id = %q; want flights.v1.plugin.search (spec §4.1 line 366)", op.DefaultVariantID)
	}
	if got := len(op.Variants); got != 1 {
		t.Fatalf("variants = %d; want 1", got)
	}

	v := op.Variants[0]
	if v.VariantID != "flights.v1.plugin.search" {
		t.Errorf("variant_id = %q; want flights.v1.plugin.search", v.VariantID)
	}
	if v.InterfaceKind != catalog.InterfaceKindPluginMCP {
		t.Errorf("interface_kind = %q; want plugin-mcp (spec §8.2 / spec line 1583)", v.InterfaceKind)
	}
	if v.BackendKind != catalog.BackendKindMCPPlugin {
		t.Errorf("backend_kind = %q; want mcp-plugin (spec §8.2 / spec line 1582)", v.BackendKind)
	}
	if v.RiskClass != catalog.RiskClassRead {
		t.Errorf("risk_class = %q; want read (spec §4.1 line 366: readonly=true)", v.RiskClass)
	}
	if v.AuthStrategy != catalog.AuthStrategyPluginManaged {
		t.Errorf("auth_strategy = %q; want plugin_managed (plugin owns its own credentials per spec §8.3)", v.AuthStrategy)
	}
	if v.OutputProfile != "flights.search.v1" {
		t.Errorf("output_profile = %q; want flights.search.v1 (spec §4.1 line 366, §8.2 line 1588)", v.OutputProfile)
	}
	if !v.Preferred {
		t.Error("preferred = false; want true (single-variant default)")
	}

	if v.Binding == nil {
		t.Fatal("binding is nil; plugin variants need a Binding to resolve adapter_key + tool_name")
	}
	// AdapterKey routes to the host's mcp-plugin executor (gum-ikg wires the
	// adapter; this test pins the key the executor must register under).
	if v.Binding.AdapterKey != "plugin.mcp" {
		t.Errorf("binding.adapter_key = %q; want plugin.mcp", v.Binding.AdapterKey)
	}
	// tool_name MUST equal the MCP-registered convenience tool (spec line 1578).
	// Plugin host uses this to dispatch tools/call against the subprocess.
	if v.Binding.ToolName != "flights_search" {
		t.Errorf("binding.tool_name = %q; want flights_search (spec line 1578: tool_name = \"flights_search\")", v.Binding.ToolName)
	}
	if v.Binding.PluginName != "google-flights" {
		t.Errorf("binding.plugin_name = %q; want google-flights (spec §8.2 line 1538: plugin.name)", v.Binding.PluginName)
	}
	if v.Binding.OperationKey != "flights.search" {
		t.Errorf("binding.operation_key = %q; want flights.search", v.Binding.OperationKey)
	}
}

// TestBuildFlightsOpValidates builds a single-op catalog and asserts it
// passes catalog.Validate so generator-side schema checks won't reject the
// plugin variant once it's appended to the embedded snapshot.
func TestBuildFlightsOpValidates(t *testing.T) {
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "test",
		Ops:                  []catalog.Op{BuildFlightsOp()},
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("validate flights-only catalog: %v", err)
	}
}

// TestBuildFlightsOpBindingValidatesAsPlugin asserts the plugin-shape binding
// passes plugins.ValidateBinding (spec §5.1 + spec §8.2 line 1593), so the
// install/dispatch gates accept this catalog variant when the fli plugin
// activates.
func TestBuildFlightsOpBindingValidatesAsPlugin(t *testing.T) {
	op := BuildFlightsOp()
	v := op.Variants[0]
	if v.Binding == nil {
		t.Fatal("binding is nil")
	}
	// plugin binding validation: mcp-plugin requires tool_name (asserted by
	// internal/plugins/binding_validation.go ValidateBinding). We re-state
	// here so this gen-catalog-side test fails fast when the manifest drift
	// would otherwise only surface at install time.
	if v.BackendKind == catalog.BackendKindMCPPlugin && v.Binding.ToolName == "" {
		t.Error("backend_kind=mcp-plugin but binding.tool_name is empty; spec §8.2 requires tool_name for mcp-plugin")
	}
}
