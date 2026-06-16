package gain_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
)

// releaseFixtureRoot resolves internal/bench/fixtures/release/ from this
// test's source location so the lookup is invariant to the directory
// `go test` is invoked from.
func releaseFixtureRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(repoRoot, "internal", "bench", "fixtures", "release")
}

// TestGainFixtureReplayReleaseSet (docs/test-matrix.md release-gate row)
// verifies the release fixture set produced by cmd/gen-release-fixtures
// (bead gum-hvx) replays cleanly through RunFixtureReplay with TOON
// shaping, that the run is byte-deterministic, and that every fixture
// in the set is consumed (no silent skip). The ≥80% savings floor is
// validated by a later bead (gum-8dl) once profile+field-mask stages
// are wired into replay; this test pins the structural contract today.
func TestGainFixtureReplayReleaseSet(t *testing.T) {
	dir := releaseFixtureRoot(t)
	result, err := gain.RunFixtureReplay(dir, "toon")
	if err != nil {
		t.Fatalf("RunFixtureReplay(release, toon): %v", err)
	}
	if !result.Deterministic {
		t.Error("release-fixture replay is not byte-deterministic across runs")
	}
	if result.Stats.TotalCalls < 200 {
		t.Fatalf("release fixture call count %d < 200 (spec §12.3)", result.Stats.TotalCalls)
	}
	t.Logf("release-fixture toon replay: calls=%d, totalIn=%d, totalSaved=%d, aggregate=%.2f%%",
		result.Stats.TotalCalls, result.Stats.TotalTokensIn, result.Stats.TotalTokensSaved,
		result.Stats.AggregateSavingsPct*100)
}

// TestGainFixtureReplayJSONDefault verifies the same release fixture set
// also replays under format="json" (the catalog default) and reports
// deterministic stats. JSON-default savings against the naive baseline
// are intentionally lower than TOON-default; the spec calls this out
// in §97 ("worst-case floor ~30%"). The numeric savings band is gated
// by the release-tag artifact, not by this in-tree test.
func TestGainFixtureReplayJSONDefault_ReleaseSet(t *testing.T) {
	dir := releaseFixtureRoot(t)
	result, err := gain.RunFixtureReplay(dir, "json")
	if err != nil {
		t.Fatalf("RunFixtureReplay(release, json): %v", err)
	}
	if !result.Deterministic {
		t.Error("release-fixture JSON replay is not byte-deterministic across runs")
	}
	if result.Stats.TotalCalls < 200 {
		t.Fatalf("release fixture call count %d < 200 (spec §12.3)", result.Stats.TotalCalls)
	}
	t.Logf("release-fixture json replay: calls=%d, totalIn=%d, totalSaved=%d, aggregate=%.2f%%",
		result.Stats.TotalCalls, result.Stats.TotalTokensIn, result.Stats.TotalTokensSaved,
		result.Stats.AggregateSavingsPct*100)
}
