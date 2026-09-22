// Package mcp — gum-26nz: the session snapshot carries active plugin ops.
//
// The merge happens in cmd/gum before Server.Run; these tests pin what the
// MCP surface does with the result. Spec §4.1 line 383 forbids
// tools/list_changed and dynamic Tier B materialisation, so a plugin op that
// enters the snapshot MUST stay a catalog record: describable, searchable,
// readable as a resource, and never a tool.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/catalog"
)

// snapshotWithPluginOp returns the base snapshot and the same snapshot with
// one active plugin variant merged in, built through the production merge.
func snapshotWithPluginOp(t *testing.T) (base, merged *catalog.Catalog) {
	t.Helper()

	base = &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops: []catalog.Op{{
			OpID: "test.read", OpSchemaVersion: 1,
			Title: "Read a thing", Summary: "Reads a thing.", DefaultVariantID: "test.read.v1",
			Variants: []catalog.Variant{{
				VariantID: "test.read.v1", VariantSchemaVersion: 1,
				Stability: catalog.StabilityStable, InterfaceKind: catalog.InterfaceKindDiscoveryREST,
				BackendKind: catalog.BackendKindDiscoveryREST, RiskClass: catalog.RiskClassRead,
			}},
		}},
	}

	pc := &catalog.PluginCatalog{PluginCatalogSchemaVersion: 1, Variants: []any{
		map[string]any{
			"variant_id":   "plug.acme.do_thing.v1",
			"op_id":        "plug.acme.do_thing",
			"owner_plugin": "acme",
			"risk_class":   "read",
			"binding": map[string]any{
				"binding_schema_version": 1,
				"adapter_key":            "plugin.mcp",
				"operation_key":          "plug.acme.do_thing",
				"plugin_name":            "acme",
				"tool_name":              "do_thing",
			},
		},
	}}

	merged, refused := catalog.MergePluginVariants(base, pc, map[string]bool{"acme": true})
	if len(refused) != 0 {
		t.Fatalf("MergePluginVariants refused=%v; want none", refused)
	}
	return base, merged
}

// listToolNames drives a live server over in-memory transports and returns
// every advertised tool name.
func listToolNames(t *testing.T, snap *catalog.Catalog) map[string]bool {
	t.Helper()

	srv := NewServerWithCatalog(describeOpDispatcher{}, snap)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, srvTransport) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	names := map[string]bool{}
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		names[tool.Name] = true
	}
	if len(names) == 0 {
		t.Fatal("tools/list returned nothing; the roster failed to register")
	}
	return names
}

// TestPluginOpsNeverBecomeTools is the gum-26nz criterion-2 proof and keeps
// test-matrix row 26 true: merging plugin variants into the snapshot MUST NOT
// change the advertised tool roster, because tool registration reads the
// static Tier A roster and never the snapshot.
func TestPluginOpsNeverBecomeTools(t *testing.T) {
	base, merged := snapshotWithPluginOp(t)

	before := listToolNames(t, base)
	after := listToolNames(t, merged)

	if len(before) != len(after) {
		t.Errorf("tool count changed after the merge: %d → %d", len(before), len(after))
	}
	for name := range after {
		if !before[name] {
			t.Errorf("merging a plugin op added tool %q", name)
		}
	}
	for _, forbidden := range []string{"plug.acme.do_thing", "acme_do_thing", "do_thing"} {
		if after[forbidden] {
			t.Errorf("plugin tool %q reached tools/list", forbidden)
		}
	}
}

// TestPluginOpIsDescribable proves the merge is what the discovery surfaces
// read: gum.describe_op resolves the plugin op from the snapshot instead of
// returning OP_NOT_FOUND.
func TestPluginOpIsDescribable(t *testing.T) {
	_, merged := snapshotWithPluginOp(t)

	got := callDescribeOp(t, merged, "plug.acme.do_thing")
	if got["op_id"] != "plug.acme.do_thing" {
		t.Fatalf("op_id=%v; want plug.acme.do_thing (body=%v)", got["op_id"], got)
	}
}

// TestPluginOpIsSearchable pins spec §2765: an installed plugin's tools are
// reachable through gum.search_apis, which indexes the snapshot.
func TestPluginOpIsSearchable(t *testing.T) {
	_, merged := snapshotWithPluginOp(t)
	s := NewServerWithCatalog(describeOpDispatcher{}, merged)

	args, _ := json.Marshal(map[string]any{"query": "do_thing acme"})
	req := &sdkmcp.CallToolRequest{Params: &sdkmcp.CallToolParamsRaw{Arguments: args}}

	res, err := s.handleSearchAPIs(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSearchAPIs: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("handleSearchAPIs returned no content")
	}
	text, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0]=%T; want *sdkmcp.TextContent", res.Content[0])
	}
	if !strings.Contains(text.Text, "plug.acme.do_thing") {
		t.Errorf("search results do not mention the plugin op:\n%s", text.Text)
	}
}

// TestPluginOpResourceReadsFromSnapshot proves gum://op/{id} takes the
// snapshot branch for an active plugin, so the inactive-plugin fallback in
// inactive_plugin_resource.go is reached only for non-active rows.
func TestPluginOpResourceReadsFromSnapshot(t *testing.T) {
	_, merged := snapshotWithPluginOp(t)
	s := NewServerWithCatalog(describeOpDispatcher{}, merged)

	req := &sdkmcp.ReadResourceRequest{Params: &sdkmcp.ReadResourceParams{URI: "gum://op/plug.acme.do_thing"}}
	res, err := s.handleOpRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleOpRead: %v", err)
	}
	if res == nil || len(res.Contents) == 0 {
		t.Fatal("handleOpRead returned no contents")
	}
	var op catalog.Op
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &op); err != nil {
		t.Fatalf("decode gum://op body: %v", err)
	}
	if op.OpID != "plug.acme.do_thing" || len(op.Variants) != 1 {
		t.Errorf("op=%+v; want the full plugin record", op)
	}
	if op.Variants[0].Binding == nil || op.Variants[0].Binding.ToolName != "do_thing" {
		t.Errorf("binding=%+v; want tool_name do_thing", op.Variants[0].Binding)
	}
}
