package gain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/gain"
)

// TestRunFixtureReplayWithShaperUsesShapedTokens verifies that when a
// Shaper returns a non-empty Body, the inner per-call savings
// calculation tokenizes the shaped bytes rather than the raw TOON
// re-encoding. With an aggressive 1-byte shaper the aggregate savings
// must approach 100% — proving the shaper path is the one being
// measured. This is the wiring point bench.ComputeReleaseSavings
// relies on (bead gum-wqk4).
func TestRunFixtureReplayWithShaperUsesShapedTokens(t *testing.T) {
	dir := fixtureDir(t)

	shaper := func(opID, format string, rawBody []byte) (gain.ShapeResult, error) {
		return gain.ShapeResult{
			Body:            []byte("x"),
			OutputProfile:   "test/aggressive",
			FieldMaskStatus: "applied",
		}, nil
	}

	res, err := gain.RunFixtureReplayWithShaper(dir, "toon", shaper)
	if err != nil {
		t.Fatalf("RunFixtureReplayWithShaper: %v", err)
	}
	if res.Stats.TotalCalls == 0 {
		t.Fatal("no calls recorded")
	}
	if res.Stats.AggregateSavingsPct < 0.95 {
		t.Errorf("AggregateSavingsPct=%.4f, expected >0.95 with 1-byte shaper",
			res.Stats.AggregateSavingsPct)
	}
}

// TestRunFixtureReplayWithShaperZeroResultFallsBack verifies that a
// Shaper returning a zero ShapeResult (Body=nil) leaves per-call
// savings unchanged from the raw TOON pass — the behaviour
// bench.releaseShaper relies on for op_ids with no registered
// profile.
func TestRunFixtureReplayWithShaperZeroResultFallsBack(t *testing.T) {
	dir := fixtureDir(t)

	shaper := func(opID, format string, rawBody []byte) (gain.ShapeResult, error) {
		return gain.ShapeResult{}, nil
	}

	baseline, err := gain.RunFixtureReplay(dir, "toon")
	if err != nil {
		t.Fatalf("RunFixtureReplay baseline: %v", err)
	}
	shaped, err := gain.RunFixtureReplayWithShaper(dir, "toon", shaper)
	if err != nil {
		t.Fatalf("RunFixtureReplayWithShaper: %v", err)
	}
	if baseline.Stats.TotalTokensSaved != shaped.Stats.TotalTokensSaved {
		t.Errorf("zero ShapeResult should match baseline: baseline saved=%d shaped saved=%d",
			baseline.Stats.TotalTokensSaved, shaped.Stats.TotalTokensSaved)
	}
	if baseline.Stats.TotalTokensIn != shaped.Stats.TotalTokensIn {
		t.Errorf("zero ShapeResult should match baseline TotalTokensIn: baseline=%d shaped=%d",
			baseline.Stats.TotalTokensIn, shaped.Stats.TotalTokensIn)
	}
}

// TestRunFixtureReplayWithShaperPropagatesError verifies a Shaper
// error bubbles up as a replay failure rather than being silently
// swallowed.
func TestRunFixtureReplayWithShaperPropagatesError(t *testing.T) {
	dir := fixtureDir(t)
	wantErr := errors.New("shaper boom")
	shaper := func(opID, format string, rawBody []byte) (gain.ShapeResult, error) {
		return gain.ShapeResult{}, wantErr
	}
	_, err := gain.RunFixtureReplayWithShaper(dir, "toon", shaper)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "shaper boom") {
		t.Errorf("error %q does not contain %q", err.Error(), "shaper boom")
	}
}

// TestRunFixtureReplayWithShaperEmptyFormatDefaultsToToon verifies
// the empty-format default still works through the shaper path.
func TestRunFixtureReplayWithShaperEmptyFormatDefaultsToToon(t *testing.T) {
	dir := fixtureDir(t)
	res, err := gain.RunFixtureReplayWithShaper(dir, "", nil)
	if err != nil {
		t.Fatalf("RunFixtureReplayWithShaper empty format: %v", err)
	}
	if res.Format != "toon" {
		t.Errorf("Format=%q, want %q", res.Format, "toon")
	}
}
