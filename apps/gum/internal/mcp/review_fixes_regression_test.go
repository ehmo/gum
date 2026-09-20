// Regression tests for the MCP defects found in the 2026-09 whole-repo review.
package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// searchAPIsRegressionServer builds a one-op server the search path can index.
func searchAPIsRegressionServer() *Server {
	opID := "example.read.thing"
	variantID := opID + ".v1"
	op := catalog.Op{
		OpID:             opID,
		OpSchemaVersion:  1,
		Title:            "Example Read Thing",
		Summary:          "Reads an example thing by id",
		ParamsRequired:   [][]string{{"thing_id"}},
		DefaultVariantID: variantID,
		Variants: []catalog.Variant{{
			VariantID:     variantID,
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindDiscoveryREST,
			BackendKind:   catalog.BackendKindDiscoveryREST,
			RiskClass:     catalog.RiskClassRead,
		}},
	}
	return NewServerWithCatalog(&captureDispatcher{}, minimalCatalog(op))
}

func callSearchAPIs(t *testing.T, srv *Server, k any) *sdkmcp.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"query": "read thing", "k": k})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	res, err := srv.handleSearchAPIs(context.Background(), &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: "gum.search_apis", Arguments: raw},
	})
	if err != nil {
		t.Fatalf("handleSearchAPIs returned a Go error: %v", err)
	}
	return res
}

// handlers.go: the schema bounds on k (integer, 1..20) are not enforced by the
// pinned SDK, and the handler took the caller's value verbatim. k=-1 reached
// profile.Apply as collapse_arrays.max_items=-1, which sliced arr[:-1] and
// panicked. In `gum mcp --stdio` that killed the whole session, so one hostile
// or buggy tool call ended every other in-flight call too.
func TestSearchAPIsRejectsOutOfRangeK(t *testing.T) {
	srv := searchAPIsRegressionServer()

	for _, k := range []any{-1, 0, 21, 1000, 2.5} {
		res := callSearchAPIs(t, srv, k)
		if res == nil {
			t.Fatalf("k=%v: nil result", k)
		}
		if !res.IsError {
			t.Errorf("k=%v: accepted; want an INVALID_ARGS error result", k)
			continue
		}
		tc, ok := res.Content[0].(*sdkmcp.TextContent)
		if !ok {
			t.Fatalf("k=%v: content[0] is %T; want *TextContent", k, res.Content[0])
		}
		if !strings.HasPrefix(tc.Text, "INVALID_ARGS: k ") {
			t.Errorf("k=%v: message = %q; want an INVALID_ARGS: k prefix", k, tc.Text)
		}
	}
}

// handlers.go: a valid k and an absent k must still work after the guard.
func TestSearchAPIsAcceptsInRangeAndAbsentK(t *testing.T) {
	srv := searchAPIsRegressionServer()

	for _, k := range []any{1, 5, 20, float64(7)} {
		if res := callSearchAPIs(t, srv, k); res.IsError {
			t.Errorf("k=%v rejected; want accepted", k)
		}
	}

	raw, _ := json.Marshal(map[string]any{"query": "read thing"})
	res, err := srv.handleSearchAPIs(context.Background(), &sdkmcp.CallToolRequest{
		Params: &sdkmcp.CallToolParamsRaw{Name: "gum.search_apis", Arguments: raw},
	})
	if err != nil {
		t.Fatalf("handleSearchAPIs returned a Go error: %v", err)
	}
	if res.IsError {
		t.Error("absent k rejected; want the tuning default")
	}
}

// handlers.go: a fractional page_size used to be forwarded upstream, where it
// came back as an opaque provider 400. gum owns the argument, so gum names it.
func TestGumReadRejectsFractionalPageSize(t *testing.T) {
	res := callGumReadWithPageSize(t, 25.5)
	if !res.IsError {
		t.Fatal("fractional page_size accepted; want an INVALID_ARGS error result")
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T; want *TextContent", res.Content[0])
	}
	if !strings.Contains(tc.Text, "page_size") {
		t.Errorf("message = %q; want it to name page_size", tc.Text)
	}
}

// callGumReadWithPageSize drives gum.read with one page_size value on an op that
// declares maxResults as its page-size param (Gmail's spelling).
func callGumReadWithPageSize(t *testing.T, pageSize any) *sdkmcp.CallToolResult {
	t.Helper()
	const opID = "gmail.users.messages.list"
	readOp := minimalReadOp(opID, catalog.RiskClassRead)
	readOp.RequestFields = []catalog.RequestField{
		{Name: "maxResults", Location: catalog.RequestFieldQuery, Type: "integer"},
		{Name: "pageToken", Location: catalog.RequestFieldQuery, Type: "string"},
	}
	srv := NewServerWithCatalog(&captureDispatcher{}, minimalCatalog(readOp))

	res, err := srv.handleRead(context.Background(), makeReadRequest(map[string]any{
		"op_id":     opID,
		"page_size": pageSize,
	}))
	if err != nil {
		t.Fatalf("handleRead returned a Go error: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("handleRead returned no content")
	}
	return res
}
