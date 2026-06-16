// gum-9vuq.10 acceptance: flights_search convenience tool wiring.
//
// Spec §4.1 line 366: flights_search → flights.search → flights.v1.plugin.search,
// output_profile=flights.search.v1, format=toon,json. Spec §8.2: the bundled fli
// Shape 1 plugin owns the only variant; AdapterKey="plugin.mcp" routes through
// the mcp-plugin executor (the executor itself is wired by gum-ikg).
//
// These tests pin the catalog + convenience-handler contract so the wiring
// shipped by this bead cannot regress before the live plugin lands:
//
//   - flights_search MUST route to op_id=flights.search and resolve the
//     plugin-mcp variant from the embedded catalog (not the fallback default).
//   - The convenience handler MUST stamp the read-class risk flags and reach
//     dispatchAndShape — proving the routing path is end-to-end intact.
//   - When a stub dispatcher returns a TOON-shaped body for flights.search,
//     the MCP layer surfaces it verbatim as TextContent[0] with no
//     resource_link block (recovery=none default).

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ehmo/gum/internal/dispatch"
)

// flightsCapturingDispatcher records the Invocation it receives so the test
// can assert that flights_search resolves to op_id=flights.search.
type flightsCapturingDispatcher struct {
	gotOpID  string
	gotArgs  map[string]any
	respBody []byte
}

func (d *flightsCapturingDispatcher) Dispatch(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	d.gotOpID = inv.OpID
	d.gotArgs = inv.Args
	body := d.respBody
	if body == nil {
		body = []byte(`{}`)
	}
	return &dispatch.ShapedResponse{Body: body}, nil
}

// TestFlightsSearchRoutesToCatalogOp — gum-9vuq.10 acceptance. The convenience
// handler MUST translate flights_search → catalog op_id=flights.search (spec
// §4.1 line 366). This pins the routing entry in convenienceABITable / the
// derived convenienceOpRouting map.
func TestFlightsSearchRoutesToCatalogOp(t *testing.T) {
	disp := &flightsCapturingDispatcher{}
	srv := NewServer(disp)

	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name: "flights_search",
			Arguments: json.RawMessage(`{
				"origin":"SFO",
				"destination":"JFK",
				"departureDate":"2026-07-01",
				"adults":1
			}`),
		},
	}

	handler := srv.makeConvenienceHandler("flights_search")
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned err=%v; want nil", err)
	}

	if disp.gotOpID != "flights.search" {
		t.Errorf("dispatcher saw op_id=%q; want flights.search (spec §4.1 line 366)", disp.gotOpID)
	}
	for _, k := range []string{"origin", "destination", "departureDate"} {
		if _, ok := disp.gotArgs[k]; !ok {
			t.Errorf("Invocation.Args missing required arg %q; convenience handler must forward verbatim", k)
		}
	}
}

// TestFlightsSearchVariantIsPluginMCP — gum-9vuq.10. The flights.search op in
// the embedded catalog MUST carry exactly one variant, flights.v1.plugin.search,
// with backend_kind=mcp-plugin so the dispatcher routes through the plugin
// executor (spec §4.1 line 366 + §8.2).
func TestFlightsSearchVariantIsPluginMCP(t *testing.T) {
	cat := defaultCatalog()
	if cat == nil {
		t.Fatal("defaultCatalog() returned nil; embedded catalog must load for this test")
	}
	var op *struct {
		variants []string
		backends []string
		profile  string
	}
	for _, c := range cat.Ops {
		if c.OpID != "flights.search" {
			continue
		}
		op = &struct {
			variants []string
			backends []string
			profile  string
		}{}
		for _, v := range c.Variants {
			op.variants = append(op.variants, v.VariantID)
			op.backends = append(op.backends, string(v.BackendKind))
			if v.OutputProfile != "" {
				op.profile = v.OutputProfile
			}
		}
	}
	if op == nil {
		t.Fatal("flights.search op missing from embedded catalog; gum-9vuq.10 must add it so dispatch can resolve the plugin variant")
	}
	if len(op.variants) != 1 || op.variants[0] != "flights.v1.plugin.search" {
		t.Errorf("flights.search variants = %v; want exactly [flights.v1.plugin.search] (spec §4.1 line 366)", op.variants)
	}
	if len(op.backends) != 1 || op.backends[0] != "mcp-plugin" {
		t.Errorf("flights.search backend_kinds = %v; want exactly [mcp-plugin] (spec §8.2 line 1582)", op.backends)
	}
	if op.profile != "flights.search.v1" {
		t.Errorf("flights.search output_profile = %q; want flights.search.v1 (spec §4.1 line 366)", op.profile)
	}
}

// TestFlightsSearchShapesPluginResultIntoToonText — gum-9vuq.10. With a stub
// dispatcher returning a TOON-shaped body (header/rows tuple), the MCP layer
// surfaces it on Content[0].TextContent verbatim and emits no resource_link
// (recovery=none default for read-class plugin variant). Proves the
// presentation layer does not re-shape or drop the plugin-side body.
func TestFlightsSearchShapesPluginResultIntoToonText(t *testing.T) {
	const toonBody = `[{"departure":"2026-07-01T08:00","arrival":"2026-07-01T16:30","carrier":"DL","price_usd":419}]`
	disp := &flightsCapturingDispatcher{respBody: []byte(toonBody)}
	srv := NewServer(disp)

	req := &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{
			Name: "flights_search",
			Arguments: json.RawMessage(`{"origin":"SFO","destination":"JFK","departureDate":"2026-07-01"}`),
		},
	}

	res, err := srv.makeConvenienceHandler("flights_search")(context.Background(), req)
	if err != nil {
		t.Fatalf("handler err=%v", err)
	}
	if res.IsError {
		t.Fatalf("res.IsError=true; want false. Content=%+v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("empty Content; convenience handler must surface the shaped body")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] type=%T; want *TextContent", res.Content[0])
	}
	if !strings.Contains(tc.Text, "carrier") || !strings.Contains(tc.Text, "DL") {
		t.Errorf("Content[0].Text=%q; want the plugin-side TOON body verbatim", tc.Text)
	}
	for _, c := range res.Content {
		if _, ok := c.(*sdkmcp.ResourceLink); ok {
			t.Error("found resource_link block; flights variant defaults to recovery=none so none should be emitted")
		}
	}
}
