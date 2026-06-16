package bench

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ehmo/gum/internal/output/gain"
	"github.com/ehmo/gum/internal/output/profile"
	"github.com/ehmo/gum/internal/output/toon"
)

// ReleaseSavings is the report ComputeReleaseSavings returns for the
// in-tree release fixture set. It is the wire-shape backing
// TestGainReleaseFixtureSavingsFloor (docs/test-matrix.md row 140 /
// bead gum-wqk4): the assertion is AggregateSavingsPct >= 0.80.
type ReleaseSavings struct {
	// Fixtures is the number of leaf fixture directories replayed.
	Fixtures int

	// NaiveToolsListTokens is tok(NaiveToolsListJSON(catalog)) — the
	// spec §2 one-time registration overhead a naive MCP server pays.
	NaiveToolsListTokens int

	// GumToolsListTokens is tok(gumToolsListJSON) the caller supplied,
	// representing GUM's 9-meta + 18-convenience tools/list reply
	// (spec §2 line 129; tier-A budget gate).
	GumToolsListTokens int

	// NaiveResponseTokensSum is the sum across fixtures of the raw
	// response.json token count — the per-call cost a naive server
	// passes through verbatim (spec §2 NaiveResponseProcessor).
	NaiveResponseTokensSum int

	// GumShapedResponseTokensSum is the sum across fixtures of the
	// shaped (profile.Apply + TOON) response token count — GUM's
	// per-call cost.
	GumShapedResponseTokensSum int

	// NaiveTotalTokens = NaiveToolsListTokens + NaiveResponseTokensSum.
	NaiveTotalTokens int

	// GumTotalTokens = GumToolsListTokens + GumShapedResponseTokensSum.
	GumTotalTokens int

	// AggregateSavingsPct = 1 - GumTotalTokens/NaiveTotalTokens. The
	// spec §1/§2 release gate fires when this drops below 0.80.
	AggregateSavingsPct float64

	// ReplayResult is the per-call gain.ReplayResult returned by the
	// inner shaped replay pass. Callers use this to surface
	// Deterministic and the per-call Stats (P50/P95/P99 etc.).
	ReplayResult gain.ReplayResult
}

// ComputeReleaseSavings replays fixtureDir through gain with the
// release-profile shaper, sums per-call shaped vs raw response tokens,
// adds the supplied registration overheads on each side, and returns
// the spec §1/§2 aggregate savings number.
//
// naiveToolsListJSON is the wire-shape tools/list payload for the
// naive MCP baseline; pass NaiveToolsListJSON(c) here.
// gumToolsListJSON is the wire-shape tools/list payload for GUM's
// 9 meta + 18 convenience tools (the caller is responsible for
// rendering it — typically via the in-memory MCP server in tests).
func ComputeReleaseSavings(fixtureDir string, naiveToolsListJSON, gumToolsListJSON []byte) (*ReleaseSavings, error) {
	if len(naiveToolsListJSON) == 0 {
		return nil, fmt.Errorf("bench: ComputeReleaseSavings: empty naiveToolsListJSON")
	}
	if len(gumToolsListJSON) == 0 {
		return nil, fmt.Errorf("bench: ComputeReleaseSavings: empty gumToolsListJSON")
	}

	naiveTL, err := gain.MeasureTokensCl100k(naiveToolsListJSON)
	if err != nil {
		return nil, fmt.Errorf("bench: ComputeReleaseSavings: tokenize naive tools/list: %w", err)
	}
	gumTL, err := gain.MeasureTokensCl100k(gumToolsListJSON)
	if err != nil {
		return nil, fmt.Errorf("bench: ComputeReleaseSavings: tokenize gum tools/list: %w", err)
	}

	rr, err := gain.RunFixtureReplayWithShaper(fixtureDir, "toon", releaseShaper)
	if err != nil {
		return nil, fmt.Errorf("bench: ComputeReleaseSavings: replay: %w", err)
	}

	rawSum := int(rr.Stats.TotalTokensIn)
	shapedSum := rawSum - int(rr.Stats.TotalTokensSaved)

	naiveTotal := naiveTL + rawSum
	gumTotal := gumTL + shapedSum

	var pct float64
	if naiveTotal > 0 {
		pct = 1.0 - float64(gumTotal)/float64(naiveTotal)
	}

	return &ReleaseSavings{
		Fixtures:                   int(rr.Stats.TotalCalls),
		NaiveToolsListTokens:       naiveTL,
		GumToolsListTokens:         gumTL,
		NaiveResponseTokensSum:     rawSum,
		GumShapedResponseTokensSum: shapedSum,
		NaiveTotalTokens:           naiveTotal,
		GumTotalTokens:             gumTotal,
		AggregateSavingsPct:        pct,
		ReplayResult:               rr,
	}, nil
}

// releaseShaper is the gain.Shaper that ComputeReleaseSavings injects.
// gum_parallel is special-cased so each result's `data` is shaped
// using that element's own op profile; everything else runs the
// per-op profile + TOON compaction pipeline.
func releaseShaper(opID, format string, rawBody []byte) (gain.ShapeResult, error) {
	_ = format
	if opID == "gum_parallel" {
		return shapeParallel(rawBody)
	}
	body, name, ok := shapeOne(opID, rawBody)
	if !ok {
		return gain.ShapeResult{}, nil
	}
	return gain.ShapeResult{Body: body, OutputProfile: name, FieldMaskStatus: "applied"}, nil
}

// shapeOne applies the per-op profile to one response body and runs
// the TOON compaction pass (tabular unwrap + deep-flatten of nested
// scalar-only maps so row columns are scalars instead of JSON blobs).
// Returns (nil, "", false) when no profile is registered for opID.
func shapeOne(opID string, raw []byte) ([]byte, string, bool) {
	p := ProfileForReleaseOp(opID)
	if p == nil {
		return nil, "", false
	}
	out, err := profile.Apply(p, profile.ApplyInput{Body: raw, UserFormat: "json"})
	if err != nil {
		return raw, p.Name, true
	}
	return encodeCompact(out.Body), p.Name, true
}

// shapeParallel decomposes a gum_parallel batch envelope, shapes each
// inner result with its own op profile, and emits a flat TOON-style
// concatenation. Drops `kind`/`status` wireframe metadata and
// re-encodes each result's data block compactly.
func shapeParallel(raw []byte) (gain.ShapeResult, error) {
	var env struct {
		BatchID string `json:"batch_id"`
		Results []struct {
			OpID   string          `json:"op_id"`
			Status string          `json:"status"`
			Data   json.RawMessage `json:"data"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return gain.ShapeResult{}, nil
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "batch_id=%s\n", env.BatchID)
	for i, r := range env.Results {
		body, _, ok := shapeOne(r.OpID, r.Data)
		if !ok {
			body = encodeCompact(r.Data)
		}
		fmt.Fprintf(&buf, "\n[%d] %s\n%s\n", i, r.OpID, string(body))
	}
	return gain.ShapeResult{
		Body:            buf.Bytes(),
		OutputProfile:   "release/gum_parallel",
		FieldMaskStatus: "applied",
	}, nil
}

// encodeCompact reduces jsonBody to its most compact TOON form: when
// it is a single-key map wrapping a homogeneous-object array, the
// inner array is deep-flattened (one dot-path column per scalar leaf)
// and re-encoded as a top-level TOON table. Anything else falls
// through to plain toon.Encode.
func encodeCompact(jsonBody []byte) []byte {
	var v any
	if err := json.Unmarshal(jsonBody, &v); err != nil {
		return jsonBody
	}
	if arr, ok := unwrapSingleKeyArray(v); ok {
		flat := make([]any, len(arr))
		for i, e := range arr {
			if m, ok := e.(map[string]any); ok {
				flat[i] = flattenScalars(m)
			} else {
				flat[i] = e
			}
		}
		if enc, err := toon.Encode(flat); err == nil {
			return enc
		}
	}
	if enc, err := toon.Encode(v); err == nil {
		return enc
	}
	return jsonBody
}

// unwrapSingleKeyArray returns the inner array when v is a map with
// exactly one key whose value is a non-empty array.
func unwrapSingleKeyArray(v any) ([]any, bool) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != 1 {
		return nil, false
	}
	for _, val := range m {
		arr, ok := val.([]any)
		if !ok || len(arr) == 0 {
			return nil, false
		}
		return arr, true
	}
	return nil, false
}

// flattenScalars copies m and lifts any nested map whose values are
// all scalars (or array-of-scalar) into dot-path keys. So
// {"end":{"dateTime":"x"}} becomes {"end.dateTime":"x"}. Nested maps
// containing further maps or arrays-of-objects are left intact so the
// shape stays lossless for the consumer.
func flattenScalars(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		sub, ok := v.(map[string]any)
		if !ok || len(sub) == 0 || hasComplexValue(sub) {
			out[k] = v
			continue
		}
		for sk, sv := range sub {
			out[k+"."+sk] = sv
		}
	}
	return out
}

// hasComplexValue reports whether any value in m is itself a map or
// an array of maps — i.e., flattening would recurse beyond one level
// and inflate the column count without preserving tabular structure.
func hasComplexValue(m map[string]any) bool {
	for _, v := range m {
		switch t := v.(type) {
		case map[string]any:
			return true
		case []any:
			if len(t) > 0 {
				if _, isMap := t[0].(map[string]any); isMap {
					return true
				}
			}
		}
	}
	return false
}
