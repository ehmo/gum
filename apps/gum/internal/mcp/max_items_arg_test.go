package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMaxItemsOverrideParsing covers the max_items grammar at the MCP seam.
// JSON numbers decode to float64, so the integer arm has to accept one.
func TestMaxItemsOverrideParsing(t *testing.T) {
	cases := []struct {
		name   string
		in     any
		want   profile.MaxItemsOverride
		wantOK bool
	}{
		{"absent", nil, profile.MaxItemsOverride{}, true},
		{"all", "all", profile.MaxItemsOverride{Mode: profile.MaxItemsUnlimited}, true},
		{"ALL", "ALL", profile.MaxItemsOverride{Mode: profile.MaxItemsUnlimited}, true},
		{"json number", float64(250), profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 250}, true},
		{"int", 7, profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 7}, true},
		{"zero", float64(0), profile.MaxItemsOverride{}, false},
		{"negative", float64(-1), profile.MaxItemsOverride{}, false},
		{"fraction", 1.5, profile.MaxItemsOverride{}, false},
		{"other string", "lots", profile.MaxItemsOverride{}, false},
		{"bool", true, profile.MaxItemsOverride{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := maxItemsOverride(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("maxItemsOverride(%v) ok = %v; want %v", tc.in, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("maxItemsOverride(%v) = %+v; want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// readOnlyTestOp builds a minimal read-class op for the handler tests.
func readOnlyTestOp(opID string) catalog.Op {
	return catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            opID,
		Summary:          "test read op",
		DefaultVariantID: opID + ".v1.test",
		Variants: []catalog.Variant{{
			VariantID:     opID + ".v1.test",
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindDiscoveryREST,
			BackendKind:   catalog.BackendKindDiscoveryREST,
			RiskClass:     catalog.RiskClassRead,
		}},
	}
}

// TestMaxItemsArgReachesInvocation pins the promotion: max_items must land on
// the Invocation and must not leak into the op args, where a generated REST
// stub would see a stray field.
func TestMaxItemsArgReachesInvocation(t *testing.T) {
	const opID = "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics"
	cases := []struct {
		name string
		arg  any
		want profile.MaxItemsOverride
	}{
		{"all", "all", profile.MaxItemsOverride{Mode: profile.MaxItemsUnlimited}},
		{"number", float64(300), profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 300}},
		{"absent", nil, profile.MaxItemsOverride{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cd := &captureDispatcher{}
			srv := NewServerWithCatalog(cd, minimalCatalog(readOnlyTestOp(opID)))

			args := map[string]any{"op_id": opID, "format": "json"}
			if tc.arg != nil {
				args["max_items"] = tc.arg
			}
			raw, _ := json.Marshal(args)
			req := &sdkmcp.CallToolRequest{
				Params: &sdkmcp.CallToolParamsRaw{Name: "gum.read", Arguments: raw},
			}
			if _, err := srv.handleRead(context.Background(), req); err != nil {
				t.Fatalf("handleRead: %v", err)
			}
			if len(cd.Calls) != 1 {
				t.Fatalf("dispatcher Calls=%d; want 1", len(cd.Calls))
			}
			inv := cd.Calls[0]
			if inv.MaxItems != tc.want {
				t.Errorf("Invocation.MaxItems = %+v; want %+v", inv.MaxItems, tc.want)
			}
			if _, leaked := inv.Args["max_items"]; leaked {
				t.Error("max_items leaked into the op args")
			}
		})
	}
}

// TestMaxItemsArgRejectsBadValue: a client that ignores the schema gets
// INVALID_ARGS before any upstream request, not silent shaping.
func TestMaxItemsArgRejectsBadValue(t *testing.T) {
	const opID = "drive.files.list"
	cd := &captureDispatcher{}
	srv := NewServerWithCatalog(cd, minimalCatalog(readOnlyTestOp(opID)))

	raw, _ := json.Marshal(map[string]any{"op_id": opID, "max_items": "some"})
	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: "gum.read", Arguments: raw},
	}
	res, err := srv.handleRead(context.Background(), req)
	if err != nil {
		t.Fatalf("handleRead go err: %v", err)
	}
	if len(cd.Calls) != 0 {
		t.Fatalf("dispatcher ran %d times despite an invalid max_items", len(cd.Calls))
	}
	body := errBodyOf(t, res)
	if !strings.Contains(body, "INVALID_ARGS") || !strings.Contains(body, "max_items") {
		t.Errorf("body=%q; want an INVALID_ARGS envelope naming max_items", body)
	}
}

// TestMaxItemsSchemaAcceptsBothForms holds the advertised schema to the two
// forms the handler accepts. A schema that rejects "all" makes the escape hatch
// unreachable from a conforming client, because additionalProperties is false.
func TestMaxItemsSchemaAcceptsBothForms(t *testing.T) {
	for _, tool := range []string{"gum.read", "gum.write", "gum.destructive"} {
		t.Run(tool, func(t *testing.T) {
			var schema map[string]any
			if err := json.Unmarshal(metaToolSchema(tool), &schema); err != nil {
				t.Fatalf("unmarshal schema: %v", err)
			}
			props, _ := schema["properties"].(map[string]any)
			prop, ok := props["max_items"].(map[string]any)
			if !ok {
				t.Fatalf("%s schema has no max_items property", tool)
			}
			alts, ok := prop["oneOf"].([]any)
			if !ok || len(alts) != 2 {
				t.Fatalf("max_items = %v; want a two-branch oneOf", prop)
			}
			intBranch, _ := alts[0].(map[string]any)
			if intBranch["type"] != "integer" || intBranch["minimum"] != float64(1) {
				t.Errorf("first branch = %v; want {type:integer, minimum:1}", intBranch)
			}
			strBranch, _ := alts[1].(map[string]any)
			if strBranch["const"] != "all" {
				t.Errorf("second branch = %v; want {const:\"all\"}", strBranch)
			}
		})
	}
}
