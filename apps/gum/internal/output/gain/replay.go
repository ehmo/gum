package gain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ehmo/gum/internal/output/toon"
)

// ReplayResult holds the output of a fixture-replay run.
type ReplayResult struct {
	// Stats is the computed gain statistics across all replayed fixtures.
	Stats Stats

	// Deterministic is true when two consecutive runs produced byte-identical output.
	// This field is populated by RunFixtureReplay when it performs two runs internally.
	Deterministic bool

	// Format is the effective output format used during the replay ("toon" or "json").
	Format string
}

// fixtureTokenCounts holds the token counts for a fixture.
type fixtureTokenCounts struct {
	In      int `json:"in"`
	OutJSON int `json:"out_json"`
	OutToon int `json:"out_toon"`
}

// ShapeResult is the per-fixture output a Shaper returns. Body is the
// shaped bytes that get tokenised as ShapedTokens. OutputProfile is the
// name of the applied profile (empty when none) and is written to the
// returned Entry.OutputProfile field. FieldMaskStatus is the value
// recorded under the entry's field_mask_status column ("applied" or
// "not_applicable").
type ShapeResult struct {
	Body            []byte
	OutputProfile   string
	FieldMaskStatus string
}

// Shaper transforms a raw response body for one fixture. Replay supplies
// the catalog op_id (parsed from request.json when present, else the
// fixture path) plus the user-requested format. When the returned
// ShapeResult.Body is nil replay falls back to format-default encoding
// with no profile.
type Shaper func(opID, format string, rawBody []byte) (ShapeResult, error)

// RunFixtureReplay runs the gain fixture-replay pipeline against the fixtures in
// fixtureDir. format must be "toon" or "json"; empty defaults to "toon".
//
// RunFixtureReplay:
//  1. Reads each fixture subdirectory under fixtureDir (request.json + response.json).
//  2. Applies the output profile / TOON encoding according to format.
//  3. Counts tokens using MeasureTokensCl100k on the raw and shaped bodies.
//  4. Runs the pipeline twice and sets Deterministic = true only when the two
//     runs produce identical Stats.
//
// This function is called by `gum gain --fixture-replay` and by TestGainFixtureReplay.
func RunFixtureReplay(fixtureDir, format string) (ReplayResult, error) {
	return RunFixtureReplayWithShaper(fixtureDir, format, nil)
}

// RunFixtureReplayWithShaper is RunFixtureReplay with an injectable shaper
// hook so callers in internal/bench can apply field-mask + expression-profile
// stages before TOON encoding (bead gum-wqk4). When shape is nil the
// behaviour matches RunFixtureReplay (raw TOON/JSON, no profile).
func RunFixtureReplayWithShaper(fixtureDir, format string, shape Shaper) (ReplayResult, error) {
	if format == "" {
		format = "toon"
	}

	// Run twice to check determinism.
	result1, err := runReplay(fixtureDir, format, true, shape)
	if err != nil {
		return ReplayResult{}, err
	}
	result2, err := runReplay(fixtureDir, format, false, shape)
	if err != nil {
		return ReplayResult{}, err
	}

	deterministic := result1.Stats.TotalCalls == result2.Stats.TotalCalls &&
		result1.Stats.TotalTokensSaved == result2.Stats.TotalTokensSaved &&
		result1.Stats.P50 == result2.Stats.P50 &&
		result1.Stats.P95 == result2.Stats.P95 &&
		result1.Stats.P99 == result2.Stats.P99

	return ReplayResult{
		Stats:         result1.Stats,
		Deterministic: deterministic,
		Format:        format,
	}, nil
}

// runReplay runs a single pass of the fixture replay.
// If writeFixtures is true, it writes expected-toon.txt and expected-tokens-cl100k.json.
func runReplay(fixtureDir, format string, writeFixtures bool, shape Shaper) (ReplayResult, error) {
	// Collect every leaf directory under fixtureDir that contains a
	// response.json. This supports both the flat testdata/fixtures/gain-replay
	// layout (one level) and the hierarchical
	// internal/bench/fixtures/release/<category>/<n>/ layout (two levels).
	names, err := collectFixtureLeaves(fixtureDir)
	if err != nil {
		return ReplayResult{}, fmt.Errorf("gain replay: walk dir %s: %w", fixtureDir, err)
	}
	// Sort alphabetically for determinism.
	sort.Strings(names)

	var replayEntries []Entry
	for _, name := range names {
		dir := filepath.Join(fixtureDir, name)
		entry, err := processFixture(dir, name, format, writeFixtures, shape)
		if err != nil {
			return ReplayResult{}, fmt.Errorf("gain replay: fixture %s: %w", name, err)
		}
		replayEntries = append(replayEntries, entry)
	}

	stats := computeStats(replayEntries)
	result := ReplayResult{
		Stats:  stats,
		Format: format,
	}

	if writeFixtures && shape == nil {
		// Write expected-baseline.json. Skip when a shaper is in effect:
		// the baseline file pins the no-shape numbers and would otherwise
		// drift with every shaper change.
		if err := writeBaseline(fixtureDir); err != nil {
			return ReplayResult{}, fmt.Errorf("gain replay: write baseline: %w", err)
		}
	}

	return result, nil
}

// collectFixtureLeaves walks root and returns every directory (relative
// to root, using filepath.ToSlash for portability) that contains a
// response.json. The walk is bounded: it never descends below a
// directory that already qualifies as a leaf, so a deeply nested tree
// with a stray response.json near the top doesn't double-count.
func collectFixtureLeaves(root string) ([]string, error) {
	var leaves []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == root {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(path, "response.json")); statErr == nil {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			leaves = append(leaves, filepath.ToSlash(rel))
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return leaves, nil
}

// processFixture processes a single fixture directory. When shape is
// non-nil, the shaper is invoked with the request's op_id (extracted
// from request.json when present, else the fixture name) and its
// returned bytes drive ShapedTokens / OutputProfile / FieldMaskStatus.
func processFixture(dir, name, format string, writeFiles bool, shape Shaper) (Entry, error) {
	// Read response.json.
	responseBytes, err := os.ReadFile(filepath.Join(dir, "response.json"))
	if err != nil {
		return Entry{}, fmt.Errorf("read response.json: %w", err)
	}

	// Decode response JSON.
	var responseVal any
	if err := json.Unmarshal(responseBytes, &responseVal); err != nil {
		return Entry{}, fmt.Errorf("parse response.json: %w", err)
	}

	// Encode to TOON.
	toonBytes, err := toon.Encode(responseVal)
	if err != nil {
		return Entry{}, fmt.Errorf("encode toon: %w", err)
	}

	// Canonical JSON (compact re-marshal).
	canonicalJSON, err := json.Marshal(responseVal)
	if err != nil {
		return Entry{}, fmt.Errorf("canonical json: %w", err)
	}

	// Measure tokens.
	tokensIn, err := MeasureTokensCl100k(responseBytes)
	if err != nil {
		return Entry{}, fmt.Errorf("measure tokens in: %w", err)
	}
	tokensOutToon, err := MeasureTokensCl100k(toonBytes)
	if err != nil {
		return Entry{}, fmt.Errorf("measure tokens out toon: %w", err)
	}
	tokensOutJSON, err := MeasureTokensCl100k(canonicalJSON)
	if err != nil {
		return Entry{}, fmt.Errorf("measure tokens out json: %w", err)
	}

	if writeFiles {
		// Write expected-toon.txt.
		if err := os.WriteFile(filepath.Join(dir, "expected-toon.txt"), toonBytes, 0o644); err != nil {
			return Entry{}, fmt.Errorf("write expected-toon.txt: %w", err)
		}

		// Write expected-tokens-cl100k.json.
		counts := fixtureTokenCounts{
			In:      tokensIn,
			OutJSON: tokensOutJSON,
			OutToon: tokensOutToon,
		}
		countsBytes, err := json.Marshal(counts)
		if err != nil {
			return Entry{}, fmt.Errorf("marshal token counts: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "expected-tokens-cl100k.json"), countsBytes, 0o644); err != nil {
			return Entry{}, fmt.Errorf("write expected-tokens-cl100k.json: %w", err)
		}
	}

	// Pick output token count based on format (default path).
	var tokensOut int
	if format == "json" {
		tokensOut = tokensOutJSON
	} else {
		tokensOut = tokensOutToon
	}

	// Resolve op_id from request.json when present, else fall back to the
	// fixture path. This both fixes the Entry.OpID surface for the
	// release-fixture layout (where `name` is a path, not an op_id) and
	// gives the optional Shaper a stable lookup key.
	opID := readRequestOpID(dir)
	if opID == "" {
		opID = name
	}

	outputProfile := ""
	fieldMaskStatus := "not_applicable"
	if shape != nil {
		res, err := shape(opID, format, responseBytes)
		if err != nil {
			return Entry{}, fmt.Errorf("shape fixture: %w", err)
		}
		if res.Body != nil {
			n, err := MeasureTokensCl100k(res.Body)
			if err != nil {
				return Entry{}, fmt.Errorf("measure tokens shaped: %w", err)
			}
			tokensOut = n
			outputProfile = res.OutputProfile
			if res.FieldMaskStatus != "" {
				fieldMaskStatus = res.FieldMaskStatus
			}
		}
	}

	var outputProfilePtr *string
	if outputProfile != "" {
		outputProfilePtr = &outputProfile
	}

	return Entry{
		OpID:                   opID,
		OpFamily:               opFamilyOf(opID),
		VariantID:              nil,
		OutputProfile:          outputProfilePtr,
		ArgsHash:               "",
		AuthSubjectFingerprint: "",
		RawTokens:              tokensIn,
		ShapedTokens:           tokensOut,
		CacheStatus:            "not_applicable",
		FieldMaskStatus:        fieldMaskStatus,
		BaselineMethod:         "fixture_replay",
		BenchmarkFixtureID:     name,
	}, nil
}

// readRequestOpID returns the op_id field of request.json in dir, or ""
// when the file is absent or malformed. Replay treats this as
// best-effort metadata; an unreadable request file does not fail the
// fixture (the response side carries the savings signal).
func readRequestOpID(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		return ""
	}
	var req struct {
		OpID string `json:"op_id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return ""
	}
	return req.OpID
}

// opFamilyOf returns the op_id with the terminal method stripped, e.g.
// "gmail.users.messages.list" → "gmail.users.messages". When the id has no
// dot it is returned unchanged.
func opFamilyOf(opID string) string {
	if i := lastDot(opID); i > 0 {
		return opID[:i]
	}
	return opID
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// baselineEntry holds per-format stats for the baseline.
type baselineEntry struct {
	TotalTokensSaved int64   `json:"total_tokens_saved"`
	Mean             float64 `json:"mean"`
	P50              int64   `json:"p50"`
	P95              int64   `json:"p95"`
	P99              int64   `json:"p99"`
}

// writeBaseline writes the expected-baseline.json file.
// It computes stats for both "toon" and "json" formats independently.
func writeBaseline(fixtureDir string) error {
	toonResult, err := runReplay(fixtureDir, "toon", false, nil)
	if err != nil {
		return err
	}
	jsonResult, err := runReplay(fixtureDir, "json", false, nil)
	if err != nil {
		return err
	}

	baseline := map[string]baselineEntry{
		"toon": {
			TotalTokensSaved: toonResult.Stats.TotalTokensSaved,
			Mean:             toonResult.Stats.MeanSavingsPerCall,
			P50:              toonResult.Stats.P50,
			P95:              toonResult.Stats.P95,
			P99:              toonResult.Stats.P99,
		},
		"json": {
			TotalTokensSaved: jsonResult.Stats.TotalTokensSaved,
			Mean:             jsonResult.Stats.MeanSavingsPerCall,
			P50:              jsonResult.Stats.P50,
			P95:              jsonResult.Stats.P95,
			P99:              jsonResult.Stats.P99,
		},
	}

	b, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(fixtureDir, "expected-baseline.json"), b, 0o644)
}
