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
	"github.com/ehmo/gum/internal/output/gain"
)

// encodeToolsEnvelope renders `{"tools": ...}` with the exact encoder
// settings NaiveToolsListJSON uses, so both sides of the registration
// comparison pay the same whitespace and escaping cost.
func encodeToolsEnvelope(t *testing.T, tools any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"tools": tools}); err != nil {
		t.Fatalf("marshal gum tools/list: %v", err)
	}
	return buf.Bytes()
}

// gumToolsListViaInMemory connects an in-memory MCP client to a fresh
// gummcp.Server (stubDispatcher), calls tools/list, and returns two
// renderings of the same reply:
//
//   - wire: every field the server actually sends, output schemas
//     included. This is GUM's real session-start registration cost.
//   - naiveFieldSet: the same tools projected onto bench.NaiveTool's
//     three fields (name, description, inputSchema), which is the field
//     set the spec §2 baseline emits.
//
// The split exists because the baseline declares no output schema. It
// returns the raw upstream body unchanged (§2 item 3), so it has no
// shaped output to describe, and the catalog carries nothing to
// synthesise one from: `response_ref` is empty on all 228 ops. Output
// schemas are not a rounding term either. Measured on this checkout they
// are 21935 of GUM's 28142 registration tokens, so charging them to the
// numerator with no counterpart in the denominator decides the result.
// TestGainReleaseFixtureSavingsFloor therefore gates both renderings and
// logs both numbers (bead gum-ph5c).
func gumToolsListViaInMemory(t *testing.T) (wire, naiveFieldSet []byte) {
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

	projected := make([]bench.NaiveTool, 0, len(res.Tools))
	for _, tool := range res.Tools {
		schema, merr := json.Marshal(tool.InputSchema)
		if merr != nil {
			t.Fatalf("marshal inputSchema for %s: %v", tool.Name, merr)
		}
		projected = append(projected, bench.NaiveTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		})
	}

	return encodeToolsEnvelope(t, res.Tools), encodeToolsEnvelope(t, projected)
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

// registrationFloor gates the like-for-like comparison: the same three
// fields on both sides. Reset from a measurement of 0.8428 on this
// checkout, not chosen to clear the spec number. Output-schema growth
// does not move it, which was the point of splitting the two gates.
const registrationFloor = 0.84

// wireFloor is the spec §2 / §12.3 normative ">=80% reduction in
// MCP-layer tokens". Tool-definition schema tokens include GUM's output
// schemas, so this gate charges GUM its full wire cost against a
// baseline that carries none. It is the published claim and it must not
// slip silently.
const wireFloor = 0.80

// TestGainReleaseFixtureSavingsFloor (spec §1/§2, beads gum-wqk4 and
// gum-ph5c): asserts both savings floors on
// internal/bench/fixtures/release/. The naive registration overhead is
// the catalog-derived NaiveToolsListJSON; the GUM registration overhead
// is the live MCP server's tools/list reply (9 meta + 18 convenience
// tools). Per-call savings come from profile.Apply over the release
// profile registry (release_profiles.go).
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

	gumWire, gumNaiveFieldSet := gumToolsListViaInMemory(t)

	report, err := bench.ComputeReleaseSavings(dir, naive, gumNaiveFieldSet)
	if err != nil {
		t.Fatalf("ComputeReleaseSavings: %v", err)
	}

	if report.AggregateSavingsPct < registrationFloor {
		t.Errorf("like-for-like savings %.4f < %.2f floor\n"+
			"  both sides carry name+description+inputSchema only\n"+
			"  fixtures=%d\n"+
			"  naive: tools_list=%d response_sum=%d total=%d\n"+
			"  gum:   tools_list=%d shaped_sum=%d total=%d",
			report.AggregateSavingsPct, registrationFloor,
			report.Fixtures,
			report.NaiveToolsListTokens, report.NaiveResponseTokensSum, report.NaiveTotalTokens,
			report.GumToolsListTokens, report.GumShapedResponseTokensSum, report.GumTotalTokens)
	}

	wireTokens, err := gain.MeasureTokensCl100k(gumWire)
	if err != nil {
		t.Fatalf("tokenize gum wire tools/list: %v", err)
	}
	wireTotal := wireTokens + report.GumShapedResponseTokensSum
	wireSavings := 1.0 - float64(wireTotal)/float64(report.NaiveTotalTokens)
	if wireSavings < wireFloor {
		t.Errorf("full-wire savings %.4f < %.2f spec floor\n"+
			"  gum tools_list on the wire=%d, %d of it beyond name+description+inputSchema\n"+
			"  the spec §2 baseline declares no output schema, so every\n"+
			"  registered outputSchema byte lands on GUM's side alone.\n"+
			"  Shrink the schemas or improve response shaping; do not\n"+
			"  move this floor, it is the published claim.\n"+
			"  naive total=%d gum total=%d",
			wireSavings, wireFloor,
			wireTokens, wireTokens-report.GumToolsListTokens,
			report.NaiveTotalTokens, wireTotal)
	}

	if !report.ReplayResult.Deterministic {
		t.Error("shaped replay is not byte-deterministic across runs")
	}
	if report.Fixtures < 200 {
		t.Errorf("fixture count %d < 200 (spec §12.3 release-set composition)", report.Fixtures)
	}

	t.Logf("release-fixture savings: fixtures=%d like_for_like=%.4f full_wire=%.4f\n"+
		"  naive: tools_list=%d response_sum=%d total=%d\n"+
		"  gum:   tools_list=%d (wire %d) shaped_sum=%d total=%d (wire %d)",
		report.Fixtures, report.AggregateSavingsPct, wireSavings,
		report.NaiveToolsListTokens, report.NaiveResponseTokensSum, report.NaiveTotalTokens,
		report.GumToolsListTokens, wireTokens, report.GumShapedResponseTokensSum,
		report.GumTotalTokens, wireTotal)
}
