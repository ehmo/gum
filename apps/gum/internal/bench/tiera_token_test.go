package bench_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/dispatch"
	gummcp "github.com/ehmo/gum/internal/mcp"
	"github.com/ehmo/gum/internal/output/gain"
)

// stubDispatcher implements dispatch.Dispatcher for bench tests.
// It satisfies the interface without doing real work.
type stubDispatcher struct{}

func (stubDispatcher) Dispatch(_ context.Context, inv *dispatch.Invocation) (*dispatch.ShapedResponse, error) {
	return &dispatch.ShapedResponse{Body: []byte(`{}`), Format: "json"}, nil
}

// buildTestServer constructs a gummcp.Server backed by a stubDispatcher.
// The server registers all 27 tools (9 meta + 18 convenience).
func buildTestServer(t *testing.T) *gummcp.Server {
	t.Helper()
	return gummcp.NewServer(stubDispatcher{})
}

// toolDescriptor holds the minimal fields we care about from a listed tool.
type toolDescriptor struct {
	Name        string
	Description string
}

// listToolsViaInMemory spins up a gummcp.Server backed by a stubDispatcher,
// connects an in-memory MCP client, and returns the listed tools as descriptors.
func listToolsViaInMemory(t *testing.T, srv *gummcp.Server) []toolDescriptor {
	t.Helper()

	srvTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() {
		done <- srv.Run(ctx, srvTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "bench-test-client", Version: "0.0.1"}, nil)
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
	case runErr := <-done:
		if runErr != nil && !isCloseErrorBench(runErr) {
			t.Logf("server.Run: %v", runErr)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not stop within 3s")
	}

	out := make([]toolDescriptor, len(res.Tools))
	for i, tool := range res.Tools {
		out[i] = toolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
		}
	}
	return out
}

func isCloseErrorBench(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, tok := range []string{"eof", "closed", "cancel", "reset", "broken pipe"} {
		if strings.Contains(msg, tok) {
			return true
		}
	}
	return false
}

// catchPanicBench wraps fn in a recover to prevent panics from crashing the
// test binary. Returns (message, true) if fn panicked, ("", false) otherwise.
func catchPanicBench(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v", r)
			panicked = true
		}
	}()
	fn()
	return "", false
}

// tokenBudgetPath returns the absolute path to testdata/tier-a-token-baseline.json.
// Spec §2 fixes this filename and location; do not move it without a spec patch
// because external tooling (`gum gain --fixture-replay`, release CI workflow)
// looks for the file at this exact path.
func tokenBudgetPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// internal/bench/ → up two → apps/gum/testdata/
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "tier-a-token-baseline.json")
}

// loadBaselineTokens reads testdata/tier-a-token-baseline.json and returns the map.
func loadBaselineTokens(t *testing.T) map[string]int {
	t.Helper()
	data, err := os.ReadFile(tokenBudgetPath(t))
	if err != nil {
		t.Fatalf("read tier-a-token-baseline.json: %v", err)
	}
	var m map[string]int
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal tier-a-token-baseline.json: %v", err)
	}
	return m
}

// TestTierATokenBudget (G4.3a):
// Measures the total cl100k_base token count across all live tools registered on
// a live MCP server and asserts it does not exceed 8000 tokens.
func TestTierATokenBudget(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := buildTestServer(t)
	tools := listToolsViaInMemory(t, srv)

	if len(tools) != 29 {
		t.Errorf("tools/list returned %d tools; want 29 (27 Tier A + 2 skills helpers)", len(tools))
	}

	total := 0
	for _, tool := range tools {
		var n int
		var err error
		panicMsg, panicked := catchPanicBench(func() {
			n, err = gain.MeasureTokensCl100k([]byte(tool.Description))
		})
		if panicked {
			t.Fatalf("MeasureTokensCl100k panicked: %s — green team must implement MeasureTokensCl100k", panicMsg)
		}
		if err != nil {
			t.Errorf("MeasureTokensCl100k(%s): %v", tool.Name, err)
			continue
		}
		total += n
	}

	const budgetTokens = 8000
	if total > budgetTokens {
		t.Errorf("total cl100k tokens = %d; exceeds budget of %d", total, budgetTokens)
	}
	t.Logf("total cl100k tokens across live tools: %d (budget: %d)", total, budgetTokens)
}

// TestTierAPerToolTokenDelta (spec §2 line 129, bead gum-coo):
// Compares each tool's measured cl100k_base token count to the stored
// baseline in testdata/tier-a-token-baseline.json. The gate is
// intentionally asymmetric: any increase above the stored baseline
// fails (a PR that lifts the budget MUST carry the
// `token-budget-increase` label and bump the baseline file in the same
// PR — the label check is enforced by the release workflow, not by
// this in-tree test). Any decrease passes and emits a Logf hint so
// the baseline can be ratcheted in a follow-up PR.
//
// MISSING_BASELINE: a tool not present in the baseline fails the gate
// so the baseline cannot silently lose entries.
func TestTierAPerToolTokenDelta(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := buildTestServer(t)
	tools := listToolsViaInMemory(t, srv)

	baseline := loadBaselineTokens(t)

	seen := make(map[string]bool, len(tools))
	for _, td := range tools {
		td := td // capture
		seen[td.Name] = true
		t.Run(td.Name, func(t *testing.T) {
			var actual int
			var err error
			panicMsg, panicked := catchPanicBench(func() {
				actual, err = gain.MeasureTokensCl100k([]byte(td.Description))
			})
			if panicked {
				t.Fatalf("MeasureTokensCl100k panicked: %s", panicMsg)
			}
			if err != nil {
				t.Fatalf("MeasureTokensCl100k: %v", err)
			}

			expected, ok := baseline[td.Name]
			if !ok {
				t.Errorf("MISSING_BASELINE: tool %q not in tier-a-token-baseline.json (actual=%d); "+
					"add an entry to testdata/tier-a-token-baseline.json in the same PR",
					td.Name, actual)
				return
			}

			switch {
			case actual > expected:
				t.Errorf("TOKEN_DELTA_REGRESSION: tool %q grew %d → %d (Δ=+%d). "+
					"PRs that increase a per-tool budget MUST carry the `token-budget-increase` "+
					"label and bump testdata/tier-a-token-baseline.json in the same change "+
					"(spec §2 line 129).",
					td.Name, expected, actual, actual-expected)
			case actual < expected:
				t.Logf("RATCHET_OPPORTUNITY: tool %q shrank %d → %d (Δ=-%d); consider tightening the baseline",
					td.Name, expected, actual, expected-actual)
			default:
				t.Logf("tool %q: actual=%d, baseline=%d (no drift)", td.Name, actual, expected)
			}
		})
	}

	// Reject orphan baseline entries: if a tool was removed from
	// tools/list but its baseline lingers, the next PR could re-add
	// the name with a silently inflated budget.
	for name := range baseline {
		if !seen[name] {
			t.Errorf("ORPHAN_BASELINE: testdata/tier-a-token-baseline.json carries %q "+
				"but it is not in tools/list; remove the entry", name)
		}
	}
}

// TestAllToolNamesCount asserts that the server exposes exactly 27 tools
// through AllToolNames() and that MetaToolNames() returns exactly 9.
func TestAllToolNamesCount(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := gummcp.NewServer(stubDispatcher{})

	metaNames := srv.MetaToolNames()
	if len(metaNames) != 9 {
		t.Errorf("MetaToolNames() returned %d names; want 9", len(metaNames))
	}

	convNames := srv.ConvenienceToolNames()
	if len(convNames) != 18 {
		t.Errorf("ConvenienceToolNames() returned %d names; want 18", len(convNames))
	}

	allNames := srv.AllToolNames()
	if len(allNames) != 27 {
		t.Errorf("AllToolNames() returned %d names; want 27 (9+18)", len(allNames))
	}
}

// Ensure catalog import is used.
var _ = catalog.Catalog{}
