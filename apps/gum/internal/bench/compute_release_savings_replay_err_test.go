package bench_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/bench"
)

// TestComputeReleaseSavingsReplayErrorWraps pins ComputeReleaseSavings's
// `gain.RunFixtureReplayWithShaper err → return "bench: ...: replay: %w"`
// arm (release_savings.go:84-86). A non-existent fixtureDir trips
// filepath.WalkDir's root-stat ENOENT inside RunFixtureReplayWithShaper;
// the wrap MUST carry the "replay:" suffix so release-blog tooling can
// distinguish a missing-fixture-tree config bug from a tokenizer fault.
func TestComputeReleaseSavingsReplayErrorWraps(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := bench.ComputeReleaseSavings(missing,
		[]byte(`{"tools":[]}`), []byte(`{"tools":[]}`))
	if err == nil {
		t.Fatal("ComputeReleaseSavings(missing fixtureDir) err=nil; want replay-wrap")
	}
	if !strings.Contains(err.Error(), "replay:") {
		t.Errorf("err=%q; want 'replay:' substring", err.Error())
	}
}
