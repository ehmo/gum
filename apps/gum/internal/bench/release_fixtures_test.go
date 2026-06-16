package bench_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/bench"
)

// TestReleaseManifestLoads is a smoke test: the on-disk manifest must
// parse, declare a non-zero Total, and have Total equal to the number
// of entries (drift between the header and the entry slice would let
// the composition gate hand-wave its way to a pass).
func TestReleaseManifestLoads(t *testing.T) {
	m, err := bench.LoadReleaseManifest(bench.ReleaseFixturesDir())
	if err != nil {
		t.Fatalf("LoadReleaseManifest: %v", err)
	}
	if m.Total != len(m.Entries) {
		t.Errorf("manifest Total=%d but len(Entries)=%d", m.Total, len(m.Entries))
	}
	if m.Total == 0 {
		t.Error("manifest is empty")
	}
}

// TestGainFixtureCompositionGate enforces spec.md §12.3: total ≥200 and
// each category sits inside ±5% of its target ratio (50/20/15/15).
// Re-tightening or loosening these targets requires a spec update.
func TestGainFixtureCompositionGate(t *testing.T) {
	m, err := bench.LoadReleaseManifest(bench.ReleaseFixturesDir())
	if err != nil {
		t.Fatalf("LoadReleaseManifest: %v", err)
	}

	if m.Total < bench.SpecMinFixtureCount {
		t.Errorf("total fixtures %d < spec floor %d", m.Total, bench.SpecMinFixtureCount)
	}

	ratios := m.CategoryRatios()

	// Every spec category MUST appear and sit inside ±tolerance.
	for cat, want := range bench.SpecComposition {
		got, ok := ratios[cat]
		if !ok {
			t.Errorf("category %s missing from fixture set; ratios=%v", cat, ratios)
			continue
		}
		if math.Abs(got-want) > bench.CategoryGateTolerance {
			t.Errorf("category %s: got ratio %.4f, want %.4f ±%.2f", cat, got, want, bench.CategoryGateTolerance)
		}
	}

	// Reject unknown categories: a typo would otherwise drain budget
	// from a real bucket without tripping the per-category checks.
	for cat := range ratios {
		if _, ok := bench.SpecComposition[cat]; !ok {
			t.Errorf("unknown category %s in fixture set", cat)
		}
	}
}

// TestReleaseFixtureFilesExistOnDisk verifies every manifest entry
// points at a real directory containing request.json + response.json.
// Without this, the composition gate could pass against an empty tree.
func TestReleaseFixtureFilesExistOnDisk(t *testing.T) {
	root := bench.ReleaseFixturesDir()
	m, err := bench.LoadReleaseManifest(root)
	if err != nil {
		t.Fatalf("LoadReleaseManifest: %v", err)
	}
	for _, e := range m.Entries {
		for _, name := range []string{"request.json", "response.json"} {
			full := filepath.Join(root, e.Path, name)
			if _, err := os.Stat(full); err != nil {
				t.Errorf("manifest entry %s missing %s: %v", e.Path, name, err)
			}
		}
		// Cheap structural check: category should match the path prefix.
		if !strings.HasPrefix(e.Path, e.Category+"/") {
			t.Errorf("entry %s path/category mismatch (category=%s)", e.Path, e.Category)
		}
	}
}

// TestLoadReleaseManifestMissingFile pins the os.ReadFile error branch:
// pointing at a directory with no manifest.json surfaces a wrapped
// "bench: load release manifest" error so callers see the layer of
// origin instead of a bare filesystem error.
func TestLoadReleaseManifestMissingFile(t *testing.T) {
	_, err := bench.LoadReleaseManifest(t.TempDir())
	if err == nil {
		t.Fatal("want read error; got nil")
	}
	if !strings.Contains(err.Error(), "bench: load release manifest") {
		t.Errorf("err=%v; want 'bench: load release manifest' wrap", err)
	}
}

// TestLoadReleaseManifestUnparseable pins the json.Unmarshal error
// branch: a manifest with malformed JSON surfaces a wrapped
// "bench: parse release manifest" error.
func TestLoadReleaseManifestUnparseable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := bench.LoadReleaseManifest(dir)
	if err == nil {
		t.Fatal("want parse error; got nil")
	}
	if !strings.Contains(err.Error(), "bench: parse release manifest") {
		t.Errorf("err=%v; want 'bench: parse release manifest' wrap", err)
	}
}
