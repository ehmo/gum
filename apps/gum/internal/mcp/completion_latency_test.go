// gum-tsu: bead-named acceptance for spec §13 line 3259 completion latency
// budget. Worst-case is a 50-variant op + 100-plugin inventory, exercised
// across each completable argument: op_id, variant_id, plugin name, and
// help topic. P95 ≤ 100 ms, P99 ≤ 250 ms on linux/amd64 CI hardware. The
// in-process transport eliminates network jitter; this is the deterministic
// upper-bound floor for the latency budget.

package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/catalog"
	gummcp "github.com/ehmo/gum/internal/mcp"
)

// TestMCPCompletionLatencyWorstCase is the bead-named acceptance for gum-tsu.
//
// Spec §13 line 3259 sets a 100 ms (P95) / 250 ms (P99) budget that must
// hold for the worst-case Tier A argument: variant_id completion against
// an op with the maximum supported variant fan-out. We synthesize that
// worst case (50 variants on one op, 100 plugins in the inventory) and
// measure per-completable-arg latency over 100 samples each.
//
// linux/amd64 is the gating CI hardware per spec; other GOOS/GOARCH
// combinations run the test but treat budget overshoot as a warning so
// laptops with thermal throttling do not flake the suite.
func TestMCPCompletionLatencyWorstCase(t *testing.T) {
	defer goleak.VerifyNone(t)

	worstCaseCatalog := buildWorstCaseCatalog(t, 50)
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	seedPluginInventory(t, filepath.Join(dataHome, "gum", "default"), 100)

	ctx, cs, cleanup := connectCompletionClient(t, worstCaseCatalog)
	defer cleanup()

	cases := []struct {
		name    string
		uri     string
		argName string
		prefix  string
	}{
		// variant_id: spec §13 line 3259 calls this out as worst case.
		{"variant_id_no_prefix", "gum://variant/{id}", "id", ""},
		{"variant_id_prefix_match", "gum://variant/{id}", "id", "v"},
		// op_id: spec §13 line 3208 source.
		{"op_id_no_prefix", "gum://op/{id}", "id", ""},
		{"op_id_prefix_match", "gum://op/{id}", "id", "worstcase"},
		// plugin name: 100-entry inventory.
		{"plugin_name_no_prefix", "gum://plugin/{name}", "name", ""},
		{"plugin_name_prefix_match", "gum://plugin/{name}", "name", "p"},
		// help topic: pre-existing source kept in the worst-case run.
		{"help_topic_no_prefix", "gum://help/{topic}", "topic", ""},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			p95, p99 := measureCompletionLatency(t, ctx, cs, c.uri, c.argName, c.prefix, 100)
			if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
				if p95 > 100*time.Millisecond {
					t.Errorf("P95=%s; spec §13 line 3259 budget is 100ms (gating on linux/amd64)", p95)
				}
				if p99 > 250*time.Millisecond {
					t.Errorf("P99=%s; spec §13 line 3259 budget is 250ms (gating on linux/amd64)", p99)
				}
			} else if p95 > 100*time.Millisecond || p99 > 250*time.Millisecond {
				t.Logf("warning: P95=%s P99=%s exceeds budget on %s/%s (linux/amd64 gates; other platforms are advisory)",
					p95, p99, runtime.GOOS, runtime.GOARCH)
			}
		})
	}
}

// measureCompletionLatency runs n Complete calls against the given ref,
// returning the P95 and P99 wall-clock latencies. Bails early on any RPC
// error since latency is meaningless when the handler is unhealthy.
func measureCompletionLatency(t *testing.T, ctx context.Context, cs *sdkmcp.ClientSession, uri, argName, prefix string, n int) (time.Duration, time.Duration) {
	t.Helper()
	durations := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		_, err := cs.Complete(ctx, &sdkmcp.CompleteParams{
			Ref:      &sdkmcp.CompleteReference{Type: "ref/resource", URI: uri},
			Argument: sdkmcp.CompleteParamsArgument{Name: argName, Value: prefix},
		})
		if err != nil {
			t.Fatalf("Complete sample %d (%s prefix=%q): %v", i, uri, prefix, err)
		}
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return durations[(n*95)/100], durations[(n*99)/100]
}

// buildWorstCaseCatalog returns a single-op catalog snapshot with the
// requested number of variants. The op_id "worstcase.gum.completion" and
// variant_ids "vNNN" pattern make the worst-case rows easy to recognise in
// failure output. The catalog satisfies catalog.Validate so the snapshot
// loader treats it identically to a real generated catalog.
func buildWorstCaseCatalog(t *testing.T, variantCount int) *catalog.Catalog {
	t.Helper()
	op := catalog.Op{
		OpID:             "worstcase.gum.completion",
		OpSchemaVersion:  1,
		Title:            "Worst-case completion target",
		Summary:          "Synthetic op with maximum variant fan-out (gum-tsu).",
		DefaultVariantID: "v000",
		Variants:         make([]catalog.Variant, variantCount),
	}
	for i := 0; i < variantCount; i++ {
		op.Variants[i] = catalog.Variant{
			VariantID:            fmt.Sprintf("v%03d", i),
			VariantSchemaVersion: 1,
			Stability:            catalog.StabilityStable,
			InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
			BackendKind:          catalog.BackendKindDiscoveryREST,
			RiskClass:            catalog.RiskClassRead,
		}
	}
	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		GeneratorVersion:     "gum-tsu-fixture-1",
		Ops:                  []catalog.Op{op},
	}
	if err := cat.Validate(); err != nil {
		t.Fatalf("buildWorstCaseCatalog: catalog.Validate: %v", err)
	}
	return cat
}

// seedPluginInventory writes plugin-state.json + plugins.lock at the given
// profile dir with the requested number of plugins. Each plugin is marked
// active so it survives the §13 line 3148 installed_pending_restart filter.
func seedPluginInventory(t *testing.T, profileDir string, count int) {
	t.Helper()
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", profileDir, err)
	}
	type pluginRow map[string]any
	stateRows := make([]pluginRow, count)
	lockRows := make([]pluginRow, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("plugin-%03d", i)
		stateRows[i] = pluginRow{
			"name":   name,
			"status": "active",
		}
		lockRows[i] = pluginRow{
			"name":          name,
			"version":       "1.0.0",
			"shape":         "mcp_subprocess",
			"tos":           "accepted",
			"risk":          "read",
			"variant_count": 1,
		}
	}
	writeJSONFile(t, filepath.Join(profileDir, "plugin-state.json"), map[string]any{"plugins": stateRows})
	writeJSONFile(t, filepath.Join(profileDir, "plugins.lock"), map[string]any{"plugins": lockRows})
}

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// connectCompletionClient wires a Server backed by the supplied catalog
// snapshot through an in-memory transport. The XDG_DATA_HOME setup is
// performed by the caller before this returns so plugin-inventory reads land
// in the test tempdir.
func connectCompletionClient(t *testing.T, snapshot *catalog.Catalog) (context.Context, *sdkmcp.ClientSession, func()) {
	t.Helper()
	srv := gummcp.NewServerWithCatalog(stubDispatcher{}, snapshot)
	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx, srvTransport) }()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "gum-tsu-client", Version: "0.0.1"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("client.Connect: %v", err)
	}
	return ctx, cs, func() {
		_ = cs.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server.Run did not stop within 2s after cancel")
		}
	}
}
