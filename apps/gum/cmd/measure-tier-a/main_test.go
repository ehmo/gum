package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	measuretiera "github.com/ehmo/gum/cmd/measure-tier-a"
	"github.com/tiktoken-go/tokenizer"
)

// metaToolCeiling and convenienceToolCeiling are per-tool token budgets from spec.md §2.
const (
	metaToolCeiling       = 360
	convenienceToolCeiling = 220
	defsOverheadCeiling    = 660
	framingReserveCeiling  = 140
	totalBudget            = 8000

	// metaToolCount and convenienceToolCount are from docs/tier-a-roster.v1.json.
	metaToolCount       = 9
	convenienceToolCount = 18
)

// metaTools is the normative meta-tool list from docs/tier-a-roster.v1.json.
var metaTools = []string{
	"gum.search_apis",
	"gum.describe_op",
	"gum.read",
	"gum.write",
	"gum.destructive",
	"gum.code",
	"gum.poll",
	"gum.cache_stats",
	"gum.gain",
}

// TestTierAFitsIn8KTokens asserts that Measure() returns TotalTokens <= 8000.
func TestTierAFitsIn8KTokens(t *testing.T) {
	report, err := measuretiera.Measure()
	if err != nil {
		t.Fatalf("Measure() error: %v", err)
	}
	if report.TotalTokens > totalBudget {
		t.Errorf("TotalTokens = %d, exceeds hard budget of %d cl100k_base tokens", report.TotalTokens, totalBudget)
	}
}

// TestTierATokenPerToolCeilings asserts per-tool and overhead ceilings per spec.md §2.
func TestTierATokenPerToolCeilings(t *testing.T) {
	report, err := measuretiera.Measure()
	if err != nil {
		t.Fatalf("Measure() error: %v", err)
	}

	metaSet := make(map[string]bool, len(metaTools))
	for _, mt := range metaTools {
		metaSet[mt] = true
	}

	for tool, tokens := range report.PerTool {
		if metaSet[tool] {
			if tokens > metaToolCeiling {
				t.Errorf("meta-tool %q: %d tokens, exceeds ceiling of %d", tool, tokens, metaToolCeiling)
			}
		} else {
			// convenience tool
			if tokens > convenienceToolCeiling {
				t.Errorf("convenience tool %q: %d tokens, exceeds ceiling of %d", tool, tokens, convenienceToolCeiling)
			}
		}
	}

	if report.DefsOverhead > defsOverheadCeiling {
		t.Errorf("DefsOverhead = %d, exceeds ceiling of %d", report.DefsOverhead, defsOverheadCeiling)
	}
	if report.FramingReserve > framingReserveCeiling {
		t.Errorf("FramingReserve = %d, exceeds ceiling of %d", report.FramingReserve, framingReserveCeiling)
	}
}

// TestTierAWriteDescriptionMandatorySentence asserts that gum.write description
// contains the normative phrase and is <= 80 cl100k_base tokens per spec.md §2.
func TestTierAWriteDescriptionMandatorySentence(t *testing.T) {
	const mandatoryPhrase = "Note: high-stakes writes may require confirmation"
	const descTokenCeiling = 80

	report, err := measuretiera.Measure()
	if err != nil {
		t.Fatalf("Measure() error: %v", err)
	}

	// The report should expose a way to check descriptions; but since Measure() only
	// returns token counts, we verify via MeasureWriteDescription which the green team
	// must also expose, OR we check via the registered tool description directly.
	// For Phase 1 red-team purposes, we use MeasureWriteDescription() as the seam.
	desc, err := measuretiera.WriteDescription()
	if err != nil {
		t.Fatalf("WriteDescription() error: %v", err)
	}

	if !strings.Contains(desc, mandatoryPhrase) {
		t.Errorf("gum.write description missing mandatory phrase %q", mandatoryPhrase)
	}

	// Count tokens in the description.
	enc, err := tokenizer.Get(tokenizer.Cl100kBase)
	if err != nil {
		t.Fatalf("tokenizer.Get: %v", err)
	}
	count, err := enc.Count(desc)
	if err != nil {
		t.Fatalf("enc.Count: %v", err)
	}
	if count > descTokenCeiling {
		t.Errorf("gum.write description is %d cl100k_base tokens, exceeds ceiling of %d", count, descTokenCeiling)
	}

	// Ensure the per-tool token count from Measure() also reflects the description.
	if wt, ok := report.PerTool["gum.write"]; ok && wt == 0 {
		t.Error("gum.write token count is 0 in report")
	}
}

// TestTierARosterCardinality asserts docs/tier-a-roster.v1.json has exactly 18 convenience tools.
func TestTierARosterCardinality(t *testing.T) {
	// Walk up from this test file to the repo root to find docs/tier-a-roster.v1.json.
	_, thisFile, _, _ := runtime.Caller(0)
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	rosterPath := filepath.Join(moduleRoot, "internal", "embedded", "data", "tier-a-roster.v1.json")

	data, err := os.ReadFile(rosterPath)
	if err != nil {
		t.Fatalf("read tier-a-roster.v1.json: %v", err)
	}

	type roster struct {
		SchemaVersion    int      `json:"schema_version"`
		MetaTools        []string `json:"meta_tools"`
		ConvenienceTools []string `json:"convenience_tools"`
	}
	var r roster
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("unmarshal roster: %v", err)
	}

	if len(r.ConvenienceTools) != convenienceToolCount {
		t.Errorf("convenience_tools count = %d, want %d", len(r.ConvenienceTools), convenienceToolCount)
	}
	if len(r.MetaTools) != metaToolCount {
		t.Errorf("meta_tools count = %d, want %d", len(r.MetaTools), metaToolCount)
	}
}
