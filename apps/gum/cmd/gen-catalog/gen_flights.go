package main

import "github.com/ehmo/gum/internal/catalog"

// BuildFlightsOp returns the Tier A Flights v1 plugin operation:
//
//	read: flights.search  (variant flights.v1.plugin.search, Shape 1 mcp-plugin)
//
// Spec §4.1 line 366 and §8.2 (plugin manifest example): the bundled fli
// plugin owns the only variant. The variant carries `interface_kind=plugin-mcp`,
// `backend_kind=mcp-plugin`, `auth_strategy=plugin_managed` (the plugin
// manages credentials internally via its declared credential descriptors),
// and a Binding whose AdapterKey routes through the host's mcp-plugin
// executor. `tool_name=flights_search` matches the convenience tool name the
// host registers in MCP (spec line 1578).
//
// The output_profile name `flights.search.v1` matches the convenience ABI
// declared in internal/mcp/tier_a_abi.go and the spec §4.1 row.
//
// Required args per spec §4.1: origin, destination, departure_date.
// Optional args: return_date, adults, cabin.
//
// Live dispatch via the actual fli subprocess lives in gum-ikg
// (plugins/flights/). This catalog entry is the gate gum-9vuq.10 ships so
// the dispatcher can resolve flights.search → flights.v1.plugin.search and
// route to a registered mcp-plugin adapter. Without an installed plugin,
// dispatch surfaces SERVICE_DOWN with adapter_key in the error envelope —
// the documented "plugin not installed" failure shape.
func BuildFlightsOp() catalog.Op {
	return catalog.Op{
		OpID:             "flights.search",
		OpSchemaVersion:  1,
		Title:            "Search Flights",
		Summary:          "Search Google Flights itineraries via the bundled fli Shape 1 plugin.",
		Service:          "flights",
		ServiceFamily:    "plugin",
		DefaultVariantID: "flights.v1.plugin.search",
		Variants: []catalog.Variant{
			{
				VariantID:            "flights.v1.plugin.search",
				VariantSchemaVersion: 1,
				Version:              "v1",
				Stability:            catalog.StabilityStable,
				InterfaceKind:        catalog.InterfaceKindPluginMCP,
				BackendKind:          catalog.BackendKindMCPPlugin,
				Preferred:            true,
				RiskClass:            catalog.RiskClassRead,
				AuthStrategy:         catalog.AuthStrategyPluginManaged,
				Scopes:               []string{},
				OutputProfile:        "flights.search.v1",
				Binding: &catalog.Binding{
					BindingSchemaVersion: 1,
					AdapterKey:           "plugin.mcp",
					OperationKey:         "flights.search",
					PluginName:           "google-flights",
					ToolName:             "flights_search",
				},
			},
		},
	}
}
