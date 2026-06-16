package bench_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/bench"
	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
	gummcp "github.com/ehmo/gum/internal/mcp"
)

// gumToolsListJSONViaInMemory connects an in-memory MCP client to a
// fresh gummcp.Server (stubDispatcher), calls tools/list, and returns
// the wire-shape JSON the naive baseline test compares against. The
// envelope mirrors NaiveToolsListJSON: `{"tools": [...]}` with each
// tool's name, description, and inputSchema fields preserved.
func gumToolsListJSONViaInMemory(t *testing.T) []byte {
	t.Helper()

	srv := gummcp.NewServer(stubDispatcher{})

	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx, srvTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "release-savings-test", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("server did not stop within 3s")
	}

	envelope := map[string]any{"tools": res.Tools}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(envelope); err != nil {
		t.Fatalf("marshal gum tools/list: %v", err)
	}
	return buf.Bytes()
}

// releaseFixtureDir resolves internal/bench/fixtures/release/ from this
// file's source location so go test invocations are cwd-independent.
func releaseFixtureDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "fixtures", "release")
}

func loadEmbeddedCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	if len(embedded.CatalogJSON) == 0 {
		t.Skip("embedded.CatalogJSON empty; skipping savings floor test")
	}
	var c catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &c); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	return &c
}

// TestGainReleaseFixtureSavingsFloor (spec §1/§2, bead gum-wqk4):
// asserts that ComputeReleaseSavings clears the ≥0.80 aggregate savings
// floor on internal/bench/fixtures/release/. The naive registration
// overhead is the catalog-derived NaiveToolsListJSON; the GUM
// registration overhead is the live MCP server's tools/list reply
// (9 meta + 18 convenience tools). Per-call savings come from
// profile.Apply over the release profile registry (release_profiles.go).
func TestGainReleaseFixtureSavingsFloor(t *testing.T) {
	defer goleak.VerifyNone(t)

	embedded := loadEmbeddedCatalog(t)
	dir := releaseFixtureDir(t)

	// The embedded 17-op catalog is far too small to represent the
	// spec §2 "naive author exposes every Google API op" baseline.
	// SpecScaleNaiveCatalog pads it to a realistic full-surface
	// scale so the ≥80% aggregate savings arithmetic reflects the
	// scenario the spec describes — see SpecScaleOpsTarget for the
	// chosen scale and the rationale.
	c, err := bench.SpecScaleNaiveCatalog(embedded, dir)
	if err != nil {
		t.Fatalf("SpecScaleNaiveCatalog: %v", err)
	}

	naive, err := bench.NaiveToolsListJSON(c)
	if err != nil {
		t.Fatalf("NaiveToolsListJSON: %v", err)
	}

	gum := gumToolsListJSONViaInMemory(t)

	report, err := bench.ComputeReleaseSavings(dir, naive, gum)
	if err != nil {
		t.Fatalf("ComputeReleaseSavings: %v", err)
	}

	const floor = 0.80
	if report.AggregateSavingsPct < floor {
		t.Errorf("aggregate savings %.4f < %.2f floor\n"+
			"  fixtures=%d\n"+
			"  naive: tools_list=%d response_sum=%d total=%d\n"+
			"  gum:   tools_list=%d shaped_sum=%d total=%d",
			report.AggregateSavingsPct, floor,
			report.Fixtures,
			report.NaiveToolsListTokens, report.NaiveResponseTokensSum, report.NaiveTotalTokens,
			report.GumToolsListTokens, report.GumShapedResponseTokensSum, report.GumTotalTokens)
	}
	if !report.ReplayResult.Deterministic {
		t.Error("shaped replay is not byte-deterministic across runs")
	}
	if report.Fixtures < 200 {
		t.Errorf("fixture count %d < 200 (spec §12.3 release-set composition)", report.Fixtures)
	}

	t.Logf("release-fixture savings: fixtures=%d savings=%.4f\n"+
		"  naive: tools_list=%d response_sum=%d total=%d\n"+
		"  gum:   tools_list=%d shaped_sum=%d total=%d",
		report.Fixtures, report.AggregateSavingsPct,
		report.NaiveToolsListTokens, report.NaiveResponseTokensSum, report.NaiveTotalTokens,
		report.GumToolsListTokens, report.GumShapedResponseTokensSum, report.GumTotalTokens)
}
