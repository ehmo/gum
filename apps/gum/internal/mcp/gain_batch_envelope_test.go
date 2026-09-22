package mcp

// Test-matrix row 64 (bead gum-7oap): the release-gated savings number must be
// computed from end-to-end ledger totals, not hardcoded.
//
// handleGain emitted `"batch_envelope_overhead": int64(0)` and aliased
// `end_to_end_savings` to `savings_pct`, so the one figure §12.3 gates the
// release on was the per-op shaping figure the same section forbids using for
// release claims, and the MCP round-trip cost of every gum_parallel batch was
// reported as zero.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
	profilepkg "github.com/ehmo/gum/internal/profile"
)

// batchLedgerFixture is one gum_parallel batch written to a fresh ledger:
// the §12.3 outer sentinel plus one inner entry per element.
type batchLedgerFixture struct {
	batchID        string
	elements       int
	outerRequest   int
	outerResponse  int
	innerRaw       int
	innerShaped    int
	baselineMethod string
}

// writeBatchLedger points HOME at a temp dir and writes fx's outer + inner
// entries to the default profile's ledger, the same file handleGain reads.
func writeBatchLedger(t *testing.T, fx batchLedgerFixture) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")

	path, err := gain.DefaultPath(profilepkg.Name("default"))
	if err != nil {
		t.Fatalf("resolve ledger path: %v", err)
	}
	ledger, err := gain.NewLedger(path)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer func() { _ = ledger.Close() }()

	outer := gain.NewGumParallelOuterEntry("0f0f0f0f", fx.batchID,
		strings.Repeat("a", 64), fx.elements, fx.outerRequest, fx.outerResponse, fx.baselineMethod)
	if err := ledger.Append(outer); err != nil {
		t.Fatalf("append outer entry: %v", err)
	}

	for i := 0; i < fx.elements; i++ {
		idx := i
		variant := "gmail.users.messages.list.v1"
		profileID := "compact"
		if err := ledger.Append(gain.Entry{
			Session:                "0f0f0f0f",
			OpID:                   "gmail.users.messages.list",
			VariantID:              &variant,
			OutputProfile:          &profileID,
			ArgsHash:               strings.Repeat("b", 64),
			AuthSubjectFingerprint: "subject",
			RequestTokens:          20,
			ResponseTokens:         fx.innerShaped,
			RawTokens:              fx.innerRaw,
			ShapedTokens:           fx.innerShaped,
			CacheStatus:            "miss",
			FieldMaskStatus:        "applied",
			OpFamily:               "gmail.users.messages",
			BaselineMethod:         fx.baselineMethod,
			BatchID:                fx.batchID,
			BatchIndex:             &idx,
		}); err != nil {
			t.Fatalf("append inner entry %d: %v", i, err)
		}
	}
	return path
}

// readLedgerEntries returns every record_type="entry" line of the ledger.
func readLedgerEntries(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("decode ledger line %q: %v", line, err)
		}
		if m["record_type"] != "entry" {
			continue
		}
		out = append(out, m)
	}
	return out
}

func gainNumber(t *testing.T, body map[string]any, key string) float64 {
	t.Helper()
	raw, present := body[key]
	if !present {
		t.Fatalf("GainResult has no %q key; body: %v", key, body)
	}
	n, ok := raw.(float64)
	if !ok {
		t.Fatalf("GainResult %q = %v (%T); want a number", key, raw, raw)
	}
	return n
}

// TestGainEndToEndSavingsIncludesBatchEnvelope is the row-64 proof. One
// fixture-backed batch of 3 elements: each inner entry shapes 4000 raw tokens
// down to 400, and the outer entry costs 40+260=300 tokens of MCP envelope.
//
//	per_op_shaping_savings = (12000-1200)/12000       = 90%
//	end_to_end_savings     = (12000-1200-300)/12000   = 87.5%
//	batch_envelope_overhead = 300
//
// The row also requires batch_id linkage and element_count == inner count.
func TestGainEndToEndSavingsIncludesBatchEnvelope(t *testing.T) {
	fx := batchLedgerFixture{
		batchID:        "a1b2c3d4",
		elements:       3,
		outerRequest:   40,
		outerResponse:  260,
		innerRaw:       4000,
		innerShaped:    400,
		baselineMethod: "fixture_replay",
	}
	path := writeBatchLedger(t, fx)

	// batch_id linkage + element_count, read from the ledger itself.
	entries := readLedgerEntries(t, path)
	var outer map[string]any
	inner := 0
	for _, e := range entries {
		if e["op_family"] == "gum_parallel" {
			outer = e
			continue
		}
		inner++
		if e["batch_id"] != fx.batchID {
			t.Errorf("inner entry batch_id = %v; want %q", e["batch_id"], fx.batchID)
		}
	}
	if outer == nil {
		t.Fatal("no gum_parallel outer entry in the ledger")
	}
	if outer["batch_id"] != fx.batchID {
		t.Errorf("outer batch_id = %v; want %q", outer["batch_id"], fx.batchID)
	}
	if got, want := outer["element_count"], float64(inner); got != want {
		t.Errorf("outer element_count = %v; want %v (the number of inner entries)", got, want)
	}
	if v, present := outer["variant_id"]; !present || v != nil {
		t.Errorf("outer variant_id = %v (present=%v); §12.3 requires null", v, present)
	}

	srv := NewServer(noopDispatcher{})
	body := invokeGainExpectSuccess(t, srv)

	if got := gainNumber(t, body, "batch_envelope_overhead"); got != 300 {
		t.Errorf("batch_envelope_overhead = %v; want 300 (the outer entry's shaped tokens)", got)
	}
	endToEnd := gainNumber(t, body, "end_to_end_savings")
	if endToEnd != 87.5 {
		t.Errorf("end_to_end_savings = %v; want 87.5", endToEnd)
	}
	perOp := gainNumber(t, body, "per_op_shaping_savings")
	if perOp != 90 {
		t.Errorf("per_op_shaping_savings = %v; want 90", perOp)
	}
	if endToEnd >= perOp {
		t.Errorf("end_to_end_savings (%v) must sit below per_op_shaping_savings (%v): the outer envelope is overhead", endToEnd, perOp)
	}
}

// TestGainEndToEndSavingsExcludesEstimatedEntries pins §12.3's release-gating
// rule: entries whose baseline_method is "estimated" are not reproducible
// evidence, so they stay out of end_to_end_savings. savings_pct still reports
// them, which is what makes the two fields different numbers.
func TestGainEndToEndSavingsExcludesEstimatedEntries(t *testing.T) {
	writeBatchLedger(t, batchLedgerFixture{
		batchID:        "99887766",
		elements:       2,
		outerRequest:   30,
		outerResponse:  170,
		innerRaw:       1000,
		innerShaped:    100,
		baselineMethod: "estimated",
	})

	srv := NewServer(noopDispatcher{})
	body := invokeGainExpectSuccess(t, srv)

	if raw, present := body["end_to_end_savings"]; !present || raw != nil {
		t.Errorf("end_to_end_savings = %v (present=%v); an all-estimated ledger has no release-gating evidence, so it must be null", raw, present)
	}
	if got := gainNumber(t, body, "batch_envelope_overhead"); got != 0 {
		t.Errorf("batch_envelope_overhead = %v; want 0 (no fixture-backed outer entries)", got)
	}
	// 2000 raw in, 200 shaped out, 200 tokens of outer envelope: the window
	// total counts estimated entries, so savings_pct stays a number.
	if got := gainNumber(t, body, "savings_pct"); got != 80 {
		t.Errorf("savings_pct = %v; want 80 (estimated entries still count in the window total)", got)
	}
}

// TestGainSummarySessionsAggregatePerSession pins the §12.3 summary-mode
// contract: `sessions[]` carries one aggregate per session, with the session's
// call count, its token totals and the op families it touched. The key was
// hardcoded to `[]`, so every caller saw an empty session list whatever the
// ledger held.
func TestGainSummarySessionsAggregatePerSession(t *testing.T) {
	writeBatchLedger(t, batchLedgerFixture{
		batchID:        "51515151",
		elements:       3,
		outerRequest:   40,
		outerResponse:  260,
		innerRaw:       4000,
		innerShaped:    400,
		baselineMethod: "fixture_replay",
	})

	srv := NewServer(noopDispatcher{})
	body := invokeGainExpectSuccess(t, srv)

	if body["mode"] != "summary" {
		t.Fatalf("mode = %v; want summary", body["mode"])
	}
	raw, present := body["sessions"]
	if !present {
		t.Fatalf("summary mode dropped the sessions key: %v", body)
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("sessions = %T; want an array", raw)
	}
	if len(list) != 1 {
		t.Fatalf("sessions rows = %d; want 1 (the fixture uses one session)", len(list))
	}
	row, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("sessions[0] = %T; want an object", list[0])
	}
	if row["session"] != "0f0f0f0f" {
		t.Errorf("sessions[0].session = %v; want 0f0f0f0f", row["session"])
	}
	if got := gainNumber(t, row, "calls"); got != 4 {
		t.Errorf("sessions[0].calls = %v; want 4 (3 inner entries plus the outer)", got)
	}
	if got := gainNumber(t, row, "baseline_tokens"); got != 12000 {
		t.Errorf("sessions[0].baseline_tokens = %v; want 12000", got)
	}
	if got := gainNumber(t, row, "actual_tokens"); got != 1500 {
		t.Errorf("sessions[0].actual_tokens = %v; want 1500 (1200 shaped plus 300 envelope)", got)
	}
	families, ok := row["op_families"].([]any)
	if !ok {
		t.Fatalf("sessions[0].op_families = %T; want an array", row["op_families"])
	}
	if len(families) != 2 || families[0] != "gmail.users.messages" || families[1] != "gum_parallel" {
		t.Errorf("sessions[0].op_families = %v; want the two families sorted", families)
	}
}
