package mcp

import (
	"context"
	"sync"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embed"
)

// TestShapeSearchAPIsRowParamsRequiredNamesNotPairs pins the audit fix: the
// params_required field a gum.search_apis result advertises must be the param
// NAMES, not the first [name, type] pair (which leaked the type into the list).
func TestShapeSearchAPIsRowParamsRequiredNamesNotPairs(t *testing.T) {
	op := catalog.Op{
		OpID:             "x.y.z",
		OpSchemaVersion:  1,
		DefaultVariantID: "x.y.z.v1",
		ParamsRequired:   [][]string{{"language", "string"}, {"source", "string"}},
		Variants:         []catalog.Variant{{VariantID: "x.y.z.v1", Stability: catalog.StabilityStable}},
	}
	srv := NewServerWithCatalog(nil, minimalCatalog(op))
	row := srv.shapeSearchAPIsRow(embed.SearchResult{OpID: "x.y.z", Summary: "s"})
	pr, ok := row["params_required"].([]string)
	if !ok {
		t.Fatalf("params_required type %T, want []string", row["params_required"])
	}
	if len(pr) != 2 || pr[0] != "language" || pr[1] != "source" {
		t.Errorf("params_required = %v; want [language source] (NAMES only, not [name,type] pairs)", pr)
	}
}

// TestBuildDescribeOpResultMarksDeprecatedVariants pins the audit fix: a variant
// listed in op.DeprecatedVariantIDs is reported deprecated:true in describe_op
// (it was always false before).
func TestBuildDescribeOpResultMarksDeprecatedVariants(t *testing.T) {
	op := &catalog.Op{
		OpID:                 "x.y",
		DefaultVariantID:     "x.y.v1",
		DeprecatedVariantIDs: []string{"x.y.v0"},
		Variants: []catalog.Variant{
			{VariantID: "x.y.v1", Stability: catalog.StabilityStable},
			{VariantID: "x.y.v0", Stability: catalog.StabilityStable},
		},
	}
	res := buildDescribeOpResult(op, 0)
	got := map[string]bool{}
	for _, v := range res.Variants {
		got[v.VariantID] = v.Deprecated
	}
	if !got["x.y.v0"] {
		t.Error("x.y.v0 should be deprecated (it's in DeprecatedVariantIDs)")
	}
	if got["x.y.v1"] {
		t.Error("x.y.v1 should NOT be deprecated")
	}
}

// TestMCPServerConcurrentHandlersRaceFree hammers the MCP server's handlers from
// many goroutines at once (the server is goroutine-per-session in production).
// Run with -race, it surfaces shared-state races like the bm25 lazy-build data
// race the audit found — which was invisible because no other test fired
// concurrent calls.
func TestMCPServerConcurrentHandlersRaceFree(t *testing.T) {
	op := catalog.Op{
		OpID:             "x.read.thing",
		OpSchemaVersion:  1,
		Title:            "Read Thing",
		Summary:          "Read a thing by id",
		DefaultVariantID: "x.read.thing.v1",
		Variants: []catalog.Variant{{
			VariantID:     "x.read.thing.v1",
			Stability:     catalog.StabilityStable,
			InterfaceKind: catalog.InterfaceKindDiscoveryREST,
			BackendKind:   catalog.BackendKindTypedRestSDK,
			RiskClass:     catalog.RiskClassRead,
		}},
	}
	srv := NewServerWithCatalog(noopDispatcher{}, minimalCatalog(op))

	// Pre-build read-only requests in the test goroutine (handlers never mutate
	// the request, only re-parse the bytes), so the worker goroutines don't
	// touch *testing.T.
	searchReq := buildReq(t, map[string]any{"query": "read thing", "k": 3})
	describeReq := buildReq(t, map[string]any{"op_id": "x.read.thing"})
	statsReq := buildReq(t, map[string]any{})
	readReq := buildReq(t, map[string]any{"op_id": "x.read.thing", "args": map[string]any{}})

	const goroutines = 32
	const iters = 25
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			ctx := context.Background()
			for i := 0; i < iters; i++ {
				switch (g + i) % 4 {
				case 0:
					_, _ = srv.handleSearchAPIs(ctx, searchReq)
				case 1:
					_, _ = srv.handleDescribeOp(ctx, describeReq)
				case 2:
					_, _ = srv.handleCacheStats(ctx, statsReq)
				case 3:
					_, _ = srv.handleRead(ctx, readReq)
				}
			}
		}(g)
	}
	wg.Wait()
}
