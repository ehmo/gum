package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// ReleaseFixturesDir returns the absolute path to the release fixture set
// at internal/bench/fixtures/release/. Tests use this rather than
// hard-coding the relative path so the assertion works regardless of
// where `go test` is invoked from.
func ReleaseFixturesDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "fixtures", "release")
}

// ReleaseManifest is the on-disk index for the release fixture set. The
// gen-release-fixtures generator writes it; the composition gate reads
// it. Shape mirrors cmd/gen-release-fixtures/main.go's Manifest type;
// kept in this package to avoid the cmd package becoming an import root.
type ReleaseManifest struct {
	SchemaVersion int                    `json:"schema_version"`
	GeneratedBy   string                 `json:"generated_by"`
	Total         int                    `json:"total"`
	Entries       []ReleaseManifestEntry `json:"entries"`
}

type ReleaseManifestEntry struct {
	Path     string `json:"path"`
	Category string `json:"category"`
	OpID     string `json:"op_id"`
}

// LoadReleaseManifest reads the manifest.json at the root of the release
// fixture set. Returns a typed error if the file is missing or unparseable.
func LoadReleaseManifest(fixtureRoot string) (*ReleaseManifest, error) {
	body, err := os.ReadFile(filepath.Join(fixtureRoot, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("bench: load release manifest: %w", err)
	}
	var m ReleaseManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("bench: parse release manifest: %w", err)
	}
	return &m, nil
}

// CategoryRatios returns the per-category fraction of the total. Returns
// an empty map for empty manifests so callers can compare against zero
// without nil-deref handling.
func (m *ReleaseManifest) CategoryRatios() map[string]float64 {
	out := map[string]float64{}
	if m.Total == 0 {
		return out
	}
	counts := map[string]int{}
	for _, e := range m.Entries {
		counts[e.Category]++
	}
	for cat, c := range counts {
		out[cat] = float64(c) / float64(m.Total)
	}
	return out
}

// SpecComposition is the normative release-fixture composition pinned by
// spec.md §12.3. CategoryGateTolerance is the ±band a category's ratio
// may sit inside before the composition gate fails.
var SpecComposition = map[string]float64{
	"workspace_toon_read": 0.50,
	"gum_parallel_batch":  0.20,
	"non_workspace_read":  0.15,
	"write_destructive":   0.15,
}

// CategoryGateTolerance is the half-width of the acceptance band around
// each SpecComposition target ratio (spec §12.3 "±5%").
const CategoryGateTolerance = 0.05

// SpecMinFixtureCount is the minimum total fixtures the release set MUST
// carry per spec §12.3.
const SpecMinFixtureCount = 200
