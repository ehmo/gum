package catalog

import (
	"encoding/json"
	"errors"
	"testing"
)

// baseSnapshot is a two-op stand-in for the embedded catalog. Only the fields
// the merge reads or preserves are populated.
func baseSnapshot() *Catalog {
	return &Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-01-01T00:00:00Z",
		GeneratorVersion:     "test",
		Ops: []Op{
			{OpID: "gmail.users.messages.list", OpSchemaVersion: 1, Title: "List", Summary: "s", DefaultVariantID: "v", Variants: []Variant{{VariantID: "v", VariantSchemaVersion: 1, Stability: StabilityStable, InterfaceKind: InterfaceKindDiscoveryREST, BackendKind: BackendKindDiscoveryREST, RiskClass: RiskClassRead}}},
			{OpID: "drive.files.list", OpSchemaVersion: 1, Title: "List", Summary: "s", DefaultVariantID: "v2", Variants: []Variant{{VariantID: "v2", VariantSchemaVersion: 1, Stability: StabilityStable, InterfaceKind: InterfaceKindDiscoveryREST, BackendKind: BackendKindDiscoveryREST, RiskClass: RiskClassRead}}},
		},
	}
}

// pluginRow builds a plugin-catalog.json variants[] row in the exact shape
// internal/plugins writes, as the map[string]any that Load leaves behind.
func pluginRow(owner, tool string, mutate func(map[string]any)) map[string]any {
	opID := "plug." + owner + "." + tool
	row := map[string]any{
		"variant_id":   opID + ".v1",
		"op_id":        opID,
		"owner_plugin": owner,
		"risk_class":   "read",
		"binding": map[string]any{
			"binding_schema_version": 1,
			"adapter_key":            "plugin.mcp",
			"operation_key":          opID,
			"plugin_name":            owner,
			"tool_name":              tool,
		},
	}
	if mutate != nil {
		mutate(row)
	}
	return row
}

func pluginCatalogWith(rows ...map[string]any) *PluginCatalog {
	pc := &PluginCatalog{PluginCatalogSchemaVersion: 1}
	for _, r := range rows {
		pc.Variants = append(pc.Variants, any(r))
	}
	return pc
}

func findOpByID(c *Catalog, opID string) *Op {
	for i := range c.Ops {
		if c.Ops[i].OpID == opID {
			return &c.Ops[i]
		}
	}
	return nil
}

// TestMergePluginVariantsActiveOp is the spec §5 contract: an active
// plugin variant reaches the session snapshot as a dispatchable op.
func TestMergePluginVariantsActiveOp(t *testing.T) {
	base := baseSnapshot()
	pc := pluginCatalogWith(pluginRow("acme", "do_thing", nil))

	got, refused := MergePluginVariants(base, pc, map[string]bool{"acme": true})
	if len(refused) != 0 {
		t.Fatalf("refused=%v; want none", refused)
	}
	op := findOpByID(got, "plug.acme.do_thing")
	if op == nil {
		t.Fatalf("op plug.acme.do_thing missing from merged snapshot; ops=%d", len(got.Ops))
	}
	if op.DefaultVariantID != "plug.acme.do_thing.v1" {
		t.Errorf("default_variant_id=%q; want plug.acme.do_thing.v1", op.DefaultVariantID)
	}
	if op.ServiceFamily != pluginServiceFamily {
		t.Errorf("service_family=%q; want %q", op.ServiceFamily, pluginServiceFamily)
	}
	if len(op.Variants) != 1 {
		t.Fatalf("variants=%d; want 1", len(op.Variants))
	}
	v := op.Variants[0]
	if v.BackendKind != BackendKindMCPPlugin || v.InterfaceKind != InterfaceKindPluginMCP {
		t.Errorf("kinds=(%q,%q); want (mcp-plugin, plugin-mcp)", v.BackendKind, v.InterfaceKind)
	}
	if v.AuthStrategy != AuthStrategyPluginManaged {
		t.Errorf("auth_strategy=%q; want plugin_managed", v.AuthStrategy)
	}
	if v.Binding == nil || v.Binding.AdapterKey != "plugin.mcp" || v.Binding.ToolName != "do_thing" {
		t.Fatalf("binding=%+v; want plugin.mcp/do_thing", v.Binding)
	}
	// The merged op must survive the same validation the generated catalog
	// does. cmd/gen-catalog/profile_gate.go runs Catalog.Validate on every
	// snapshot it emits, so a merged op that fails here breaks generation.
	if err := got.Validate(); err != nil {
		t.Errorf("merged snapshot fails Validate: %v", err)
	}
}

// TestMergePluginVariantsSkipsInactive proves criterion 4: a plugin whose
// status is not active never enters the snapshot, even though its rows stay
// in plugin-catalog.json.
func TestMergePluginVariantsSkipsInactive(t *testing.T) {
	base := baseSnapshot()
	pc := pluginCatalogWith(
		pluginRow("acme", "do_thing", nil),
		pluginRow("pending", "other", nil),
	)

	got, refused := MergePluginVariants(base, pc, map[string]bool{"acme": true})
	if len(refused) != 0 {
		t.Fatalf("refused=%v; want none: an inactive plugin is not an error", refused)
	}
	if findOpByID(got, "plug.pending.other") != nil {
		t.Error("inactive plugin op reached the snapshot")
	}
	if findOpByID(got, "plug.acme.do_thing") == nil {
		t.Error("active plugin op missing from the snapshot")
	}
}

// TestMergePluginVariantsOpIDCollision pins the collision policy: the built-in
// op wins and the plugin row is reported, so a profile file cannot shadow a
// Google op.
func TestMergePluginVariantsOpIDCollision(t *testing.T) {
	base := baseSnapshot()
	row := pluginRow("acme", "do_thing", func(r map[string]any) {
		r["op_id"] = "gmail.users.messages.list"
	})
	pc := pluginCatalogWith(row)

	got, refused := MergePluginVariants(base, pc, map[string]bool{"acme": true})
	if len(refused) != 1 || !errors.Is(refused[0], ErrPluginOpIDConflict) {
		t.Fatalf("refused=%v; want one ErrPluginOpIDConflict", refused)
	}
	op := findOpByID(got, "gmail.users.messages.list")
	if op == nil || op.Variants[0].BackendKind == BackendKindMCPPlugin {
		t.Fatalf("built-in op was shadowed by the plugin row: %+v", op)
	}
	if len(got.Ops) != len(base.Ops) {
		t.Errorf("ops=%d; want %d (nothing merged)", len(got.Ops), len(base.Ops))
	}
}

// TestMergePluginVariantsPluginCollision applies the same policy between two
// plugin rows: the first row in file order keeps the op_id.
func TestMergePluginVariantsPluginCollision(t *testing.T) {
	first := pluginRow("acme", "do_thing", nil)
	second := pluginRow("other", "do_thing", func(r map[string]any) {
		r["op_id"] = "plug.acme.do_thing"
		r["variant_id"] = "plug.acme.do_thing.v1"
	})
	got, refused := MergePluginVariants(baseSnapshot(), pluginCatalogWith(first, second),
		map[string]bool{"acme": true, "other": true})

	if len(refused) != 1 || !errors.Is(refused[0], ErrPluginOpIDConflict) {
		t.Fatalf("refused=%v; want one ErrPluginOpIDConflict", refused)
	}
	op := findOpByID(got, "plug.acme.do_thing")
	if op == nil || op.Service != "acme" {
		t.Fatalf("op=%+v; want the acme row to keep the op_id", op)
	}
}

// TestMergePluginVariantsRejectsMalformed covers every field the merge needs
// to route a call. Each subtest breaks exactly one of them.
func TestMergePluginVariantsRejectsMalformed(t *testing.T) {
	cases := map[string]func(map[string]any){
		"no op_id":        func(r map[string]any) { delete(r, "op_id") },
		"no variant_id":   func(r map[string]any) { delete(r, "variant_id") },
		"no owner_plugin": func(r map[string]any) { delete(r, "owner_plugin") },
		"bad risk_class":  func(r map[string]any) { r["risk_class"] = "sudo" },
		"no binding":      func(r map[string]any) { delete(r, "binding") },
		"no adapter_key": func(r map[string]any) {
			delete(r["binding"].(map[string]any), "adapter_key")
		},
		"no tool_name": func(r map[string]any) {
			delete(r["binding"].(map[string]any), "tool_name")
		},
		// The security-relevant one: a row that binds another plugin's host
		// would run that plugin's executable under this row's risk decision.
		"binding names another plugin": func(r map[string]any) {
			r["binding"].(map[string]any)["plugin_name"] = "victim"
		},
		"binding is not an object": func(r map[string]any) { r["binding"] = 7 },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			pc := pluginCatalogWith(pluginRow("acme", "do_thing", mutate))
			got, refused := MergePluginVariants(baseSnapshot(), pc, map[string]bool{"acme": true})
			if len(refused) != 1 || !errors.Is(refused[0], ErrPluginRowMalformed) {
				t.Fatalf("refused=%v; want one ErrPluginRowMalformed", refused)
			}
			if findOpByID(got, "plug.acme.do_thing") != nil {
				t.Error("malformed row still reached the snapshot")
			}
		})
	}
}

// TestMergePluginVariantsRejectsUnmarshalableRow covers the re-marshal guard.
// Load parses the file into []any, so a row reaching the merge is normally
// JSON-derived and re-marshals cleanly. A caller that builds a PluginCatalog
// in memory can still hand over a value encoding/json refuses, and the merge
// must report that row rather than panic on it.
func TestMergePluginVariantsRejectsUnmarshalableRow(t *testing.T) {
	pc := &PluginCatalog{PluginCatalogSchemaVersion: 1, Variants: []any{make(chan int)}}

	got, refused := MergePluginVariants(baseSnapshot(), pc, map[string]bool{"acme": true})

	if len(refused) != 1 || !errors.Is(refused[0], ErrPluginRowMalformed) {
		t.Fatalf("refused=%v; want one ErrPluginRowMalformed", refused)
	}
	if len(got.Ops) != len(baseSnapshot().Ops) {
		t.Errorf("ops=%d; want the base op set unchanged", len(got.Ops))
	}
}

// TestMergePluginVariantsDoesNotMutateBase guards the shared embedded
// catalog: two profiles merging different plugin sets must not see each
// other's ops.
func TestMergePluginVariantsDoesNotMutateBase(t *testing.T) {
	base := baseSnapshot()
	before := len(base.Ops)
	got, _ := MergePluginVariants(base, pluginCatalogWith(pluginRow("acme", "do_thing", nil)),
		map[string]bool{"acme": true})

	if len(base.Ops) != before {
		t.Errorf("base ops=%d; want %d unchanged", len(base.Ops), before)
	}
	if &base.Ops[0] == &got.Ops[0] {
		t.Error("merged snapshot shares the base Ops backing array")
	}
	if got.GeneratedAt != base.GeneratedAt || got.CatalogSchemaVersion != base.CatalogSchemaVersion {
		t.Error("merged snapshot dropped the base catalog header fields")
	}
}

// TestMergePluginVariantsNoOpInputs covers the early returns: with nothing to
// merge the caller gets the base snapshot back untouched.
func TestMergePluginVariantsNoOpInputs(t *testing.T) {
	base := baseSnapshot()
	active := map[string]bool{"acme": true}
	rows := pluginCatalogWith(pluginRow("acme", "do_thing", nil))

	cases := map[string]struct {
		base   *Catalog
		pc     *PluginCatalog
		active map[string]bool
	}{
		"nil base":       {nil, rows, active},
		"nil catalog":    {base, nil, active},
		"no rows":        {base, &PluginCatalog{PluginCatalogSchemaVersion: 1}, active},
		"no active set":  {base, rows, nil},
		"empty active":   {base, rows, map[string]bool{}},
		"unknown active": {base, rows, map[string]bool{"nobody": true}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, refused := MergePluginVariants(tc.base, tc.pc, tc.active)
			if len(refused) != 0 {
				t.Errorf("refused=%v; want none", refused)
			}
			if tc.base != nil && findOpByID(got, "plug.acme.do_thing") != nil {
				t.Error("op merged from a no-op input set")
			}
		})
	}
}

// TestPluginRowRoundTripsInstallShape proves the decoder reads the bytes
// internal/plugins actually writes, not a shape invented for the test: the
// row is marshalled to JSON and parsed back through the file loader first.
func TestPluginRowRoundTripsInstallShape(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"plugin_catalog_schema_version": 1,
		"variants":                      []any{pluginRow("acme", "do_thing", nil)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	pc, err := LoadPluginCatalog(data)
	if err != nil {
		t.Fatalf("LoadPluginCatalog: %v", err)
	}
	got, refused := MergePluginVariants(baseSnapshot(), pc, map[string]bool{"acme": true})
	if len(refused) != 0 {
		t.Fatalf("refused=%v; want none", refused)
	}
	if findOpByID(got, "plug.acme.do_thing") == nil {
		t.Error("row written by the install path did not merge")
	}
}
