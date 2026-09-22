// Package catalog — session snapshot merge for plugin-owned variants
// (spec.md §5 line 405, §8.7).
//
// The build-time catalog ships inside the binary. Plugin-owned variants are
// written to the profile's plugin-catalog.json at install time, so the two
// live in different files with different lifetimes. Spec §5 says the active
// plugin variants "are loaded into the active session catalog snapshot",
// which is what MergePluginVariants does: one pure function over the two
// inputs, so the CLI and the MCP server share a single merged snapshot
// instead of each deriving its own.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Sentinel errors for plugin-catalog.json rows the merge refuses.
var (
	// ErrPluginRowMalformed marks a variants[] row that does not carry the
	// fields dispatch needs to route a call to the owning plugin.
	ErrPluginRowMalformed = errors.New("catalog: malformed plugin catalog row")
	// ErrPluginOpIDConflict marks a row whose op_id is already in the snapshot.
	ErrPluginOpIDConflict = errors.New("catalog: plugin op_id already in the snapshot")
)

// pluginServiceFamily is the service_family every plugin-owned op carries. It
// matches the bundled Flights op (cmd/gen-catalog/gen_flights.go), so a
// caller filtering the snapshot by family sees plugin ops from both sources.
const pluginServiceFamily = "plugin"

// MergePluginVariants returns a snapshot carrying base's ops plus one op per
// active plugin variant in pc.
//
// active is the set of plugin names this process may dispatch. Spec §8.7
// makes that exactly the plugin-state.json rows whose status is "active" and
// that are not quarantined; plugins.SessionCatalog computes it. A row owned
// by any other plugin is skipped without an error, because
// installed_pending_restart and needs_configuration are ordinary states.
//
// Collision policy: an op_id already in base always wins and the plugin row
// is dropped with ErrPluginOpIDConflict. The embedded catalog is generated
// and hashed with the binary, while plugin-catalog.json is a profile file any
// local process can append to, so a plugin row must never shadow a built-in
// op such as gmail.users.messages.list. Two plugin rows that claim one op_id
// resolve the same way and the first in file order keeps it; the registry
// sorts variants[] by variant_id, so the winner does not depend on which
// plugin was installed first.
//
// This is the opposite precedence from `gum catalog list-overrides`
// (cmd/gum/catalog.go), and deliberately so. That command merges
// risk_override rows keyed by variant_id, where a plugin restating the risk
// class of a variant it already owns is the whole point. Here the key is
// op_id and the question is which code a call reaches, so the generated
// catalog wins.
//
// base is never mutated: the result owns a fresh Ops slice. The second return
// value carries one error per refused row so the caller can log them without
// failing the boot.
func MergePluginVariants(base *Catalog, pc *PluginCatalog, active map[string]bool) (*Catalog, []error) {
	if base == nil || pc == nil || len(pc.Variants) == 0 || len(active) == 0 {
		return base, nil
	}

	taken := make(map[string]struct{}, len(base.Ops))
	for i := range base.Ops {
		taken[base.Ops[i].OpID] = struct{}{}
	}

	out := *base
	out.Ops = make([]Op, len(base.Ops), len(base.Ops)+len(pc.Variants))
	copy(out.Ops, base.Ops)

	var refused []error
	for _, raw := range pc.Variants {
		row, err := decodePluginVariantRow(raw)
		if err != nil {
			refused = append(refused, err)
			continue
		}
		if !active[row.OwnerPlugin] {
			continue
		}
		if _, dup := taken[row.OpID]; dup {
			refused = append(refused, fmt.Errorf("plugin %s: op_id %s: %w",
				row.OwnerPlugin, row.OpID, ErrPluginOpIDConflict))
			continue
		}
		taken[row.OpID] = struct{}{}
		out.Ops = append(out.Ops, row.op())
	}
	return &out, refused
}

// pluginVariantRow is the part of a plugin-catalog.json variants[] row the
// session snapshot needs. internal/plugins writes every field named here.
type pluginVariantRow struct {
	VariantID          string    `json:"variant_id"`
	OpID               string    `json:"op_id"`
	OwnerPlugin        string    `json:"owner_plugin"`
	RiskClass          RiskClass `json:"risk_class"`
	RiskOverride       bool      `json:"risk_override"`
	RiskOverrideReason string    `json:"risk_override_reason"`
	Binding            *Binding  `json:"binding"`
}

// decodePluginVariantRow converts one variants[] element into a typed row and
// rejects anything dispatch could not route. The round trip through JSON is
// deliberate: Load leaves the rows as map[string]any so the registry can
// rewrite a file it does not fully model, and re-marshalling is the only way
// to reach the typed Binding without duplicating its field names here.
func decodePluginVariantRow(raw any) (*pluginVariantRow, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPluginRowMalformed, err)
	}
	var row pluginVariantRow
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPluginRowMalformed, err)
	}

	switch {
	case row.OpID == "":
		return nil, fmt.Errorf("%w: missing op_id", ErrPluginRowMalformed)
	case row.VariantID == "":
		return nil, fmt.Errorf("%w: op %s: missing variant_id", ErrPluginRowMalformed, row.OpID)
	case row.OwnerPlugin == "":
		return nil, fmt.Errorf("%w: op %s: missing owner_plugin", ErrPluginRowMalformed, row.OpID)
	case !row.RiskClass.Valid():
		return nil, fmt.Errorf("%w: op %s: risk_class %q", ErrPluginRowMalformed, row.OpID, row.RiskClass)
	case row.Binding == nil:
		return nil, fmt.Errorf("%w: op %s: missing binding", ErrPluginRowMalformed, row.OpID)
	case row.Binding.AdapterKey == "":
		return nil, fmt.Errorf("%w: op %s: binding.adapter_key is empty", ErrPluginRowMalformed, row.OpID)
	case row.Binding.ToolName == "":
		return nil, fmt.Errorf("%w: op %s: binding.tool_name is empty", ErrPluginRowMalformed, row.OpID)
	}

	// The binding names the host the adapter spawns. A row that claims one
	// owner and binds another plugin's name would run plugin B's executable,
	// with plugin B's stored credentials, under plugin A's install and risk
	// decision. Refuse it here rather than at spawn time.
	if row.Binding.PluginName != row.OwnerPlugin {
		return nil, fmt.Errorf("%w: op %s: binding.plugin_name %q does not match owner_plugin %q",
			ErrPluginRowMalformed, row.OpID, row.Binding.PluginName, row.OwnerPlugin)
	}
	return &row, nil
}

// op projects the row into the Op the dispatcher resolves. What the row does
// not carry is fixed by spec §8.2: a plugin op has exactly one variant, that
// variant is Shape 1 mcp-plugin, and the plugin manages its own credentials.
//
// Stability is "stable" because the op has a single variant, so the value
// never selects anything, and the bundled Flights plugin variant already
// carries it. Title and summary are derived, not declared: plugin-catalog.json
// records no prose, and a synthesized line keeps gum.describe_op and the BM25
// index from showing an empty row.
func (r *pluginVariantRow) op() Op {
	return Op{
		OpID:             r.OpID,
		OpSchemaVersion:  1,
		Title:            r.Binding.ToolName,
		Summary:          "Tool " + r.Binding.ToolName + " advertised by plugin " + r.OwnerPlugin + ".",
		Service:          r.OwnerPlugin,
		ServiceFamily:    pluginServiceFamily,
		DefaultVariantID: r.VariantID,
		Variants: []Variant{{
			VariantID:            r.VariantID,
			VariantSchemaVersion: 1,
			Version:              "v1",
			Stability:            StabilityStable,
			InterfaceKind:        InterfaceKindPluginMCP,
			BackendKind:          BackendKindMCPPlugin,
			Preferred:            true,
			RiskClass:            r.RiskClass,
			RiskOverride:         r.RiskOverride,
			RiskOverrideReason:   r.RiskOverrideReason,
			AuthStrategy:         AuthStrategyPluginManaged,
			Scopes:               []string{},
			Binding:              r.Binding,
		}},
	}
}
