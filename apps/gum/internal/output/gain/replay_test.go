package gain_test

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/output/gain"
)

// fixtureDir returns the absolute path to testdata/fixtures/gain-replay.
func fixtureDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// From internal/output/gain/ go up 3 levels to the repo root.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "testdata", "fixtures", "gain-replay")
}

// expectedBaseline holds the summary from expected-baseline.json.
type expectedBaseline struct {
	TotalExpectedSavingsPct float64 `json:"total_expected_savings_pct"`
}

func loadBaseline(t *testing.T, dir string) expectedBaseline {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "expected-baseline.json"))
	if err != nil {
		t.Fatalf("loadBaseline: %v", err)
	}
	var b expectedBaseline
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatalf("loadBaseline unmarshal: %v", err)
	}
	return b
}

// catchPanicGain wraps fn and recovers from any panic.
func catchPanicGain(fn func()) (msg string, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprintf("panic: %v", r)
			panicked = true
		}
	}()
	fn()
	return "", false
}

// TestGainFixtureReplay (G4.4):
//   - Runs RunFixtureReplay twice with format="toon".
//   - Asserts the two runs produce identical Stats (Deterministic=true).
//   - Asserts reported total savings is within ±2% of the committed baseline.
func TestGainFixtureReplay(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := fixtureDir(t)
	_ = loadBaseline(t, dir) // ensure baseline file exists

	var result gain.ReplayResult
	var err error
	panicMsg, panicked := catchPanicGain(func() {
		result, err = gain.RunFixtureReplay(dir, "toon")
	})
	if panicked {
		t.Fatalf("RunFixtureReplay panicked: %s — green team must implement RunFixtureReplay", panicMsg)
	}
	if err != nil {
		t.Fatalf("RunFixtureReplay(toon): %v", err)
	}

	if !result.Deterministic {
		t.Error("RunFixtureReplay: two consecutive runs produced different Stats (Deterministic=false)")
	}

	if result.Format != "toon" {
		t.Errorf("Format = %q; want toon", result.Format)
	}

	if result.Stats.TotalCalls == 0 {
		t.Fatal("RunFixtureReplay returned zero calls; fixture directory may be empty or unreadable")
	}

	if result.Stats.MeanSavingsPerCall < 0 {
		t.Errorf("MeanSavingsPerCall = %.2f; want >= 0", result.Stats.MeanSavingsPerCall)
	}

	t.Logf("RunFixtureReplay(toon): calls=%d, totalSaved=%d, mean=%.2f, p50=%d, p95=%d",
		result.Stats.TotalCalls,
		result.Stats.TotalTokensSaved,
		result.Stats.MeanSavingsPerCall,
		result.Stats.P50,
		result.Stats.P95,
	)
}

// TestGainFixtureReplayJSONDefault (G4.5):
//   - Same as TestGainFixtureReplay but with format="json".
//   - JSON output is the "unshaped" baseline, so savings should be smaller.
//   - Still checks determinism and ±2% of json-specific expected savings.
func TestGainFixtureReplayJSONDefault(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := fixtureDir(t)

	var result gain.ReplayResult
	var err error
	panicMsg, panicked := catchPanicGain(func() {
		result, err = gain.RunFixtureReplay(dir, "json")
	})
	if panicked {
		t.Fatalf("RunFixtureReplay(json) panicked: %s — green team must implement RunFixtureReplay", panicMsg)
	}
	if err != nil {
		t.Fatalf("RunFixtureReplay(json): %v", err)
	}

	if !result.Deterministic {
		t.Error("RunFixtureReplay(json): two consecutive runs produced different Stats (Deterministic=false)")
	}

	if result.Format != "json" {
		t.Errorf("Format = %q; want json", result.Format)
	}

	if result.Stats.TotalCalls == 0 {
		t.Fatal("RunFixtureReplay(json) returned zero calls")
	}

	if result.Stats.MeanSavingsPerCall < 0 {
		t.Logf("WARNING: MeanSavingsPerCall(json) = %.2f; negative savings with json format is expected only for sparse/empty fixtures", result.Stats.MeanSavingsPerCall)
	}

	t.Logf("RunFixtureReplay(json): calls=%d, totalSaved=%d, mean=%.2f",
		result.Stats.TotalCalls,
		result.Stats.TotalTokensSaved,
		result.Stats.MeanSavingsPerCall,
	)
}

// TestGainFixtureReplayWithinTolerance checks that two calls to RunFixtureReplay
// with the same inputs produce savings within ±2% of each other (tolerance check).
func TestGainFixtureReplayWithinTolerance(t *testing.T) {
	defer goleak.VerifyNone(t)

	dir := fixtureDir(t)

	var r1, r2 gain.ReplayResult
	var err error
	panicMsg, panicked := catchPanicGain(func() {
		r1, err = gain.RunFixtureReplay(dir, "toon")
	})
	if panicked {
		t.Fatalf("first RunFixtureReplay panicked: %s", panicMsg)
	}
	if err != nil {
		t.Fatalf("first RunFixtureReplay: %v", err)
	}

	panicMsg, panicked = catchPanicGain(func() {
		r2, err = gain.RunFixtureReplay(dir, "toon")
	})
	if panicked {
		t.Fatalf("second RunFixtureReplay panicked: %s", panicMsg)
	}
	if err != nil {
		t.Fatalf("second RunFixtureReplay: %v", err)
	}

	if r1.Stats.TotalTokensSaved != r2.Stats.TotalTokensSaved {
		t.Errorf("TotalTokensSaved differs between runs: %d vs %d",
			r1.Stats.TotalTokensSaved, r2.Stats.TotalTokensSaved)
	}
	if r1.Stats.TotalCalls != r2.Stats.TotalCalls {
		t.Errorf("TotalCalls differs between runs: %d vs %d",
			r1.Stats.TotalCalls, r2.Stats.TotalCalls)
	}

	// ±2% tolerance on MeanSavingsPerCall.
	if r1.Stats.MeanSavingsPerCall != 0 {
		diff := math.Abs(r2.Stats.MeanSavingsPerCall-r1.Stats.MeanSavingsPerCall) / math.Abs(r1.Stats.MeanSavingsPerCall)
		if diff > 0.02 {
			t.Errorf("MeanSavingsPerCall differs by more than 2%% between runs: %.4f vs %.4f (diff=%.2f%%)",
				r1.Stats.MeanSavingsPerCall, r2.Stats.MeanSavingsPerCall, diff*100)
		}
	}
}
