package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ehmo/gum/internal/catalog"
)

// SpecScaleOpsTarget is the synthetic catalog size used by
// SpecScaleNaiveCatalog. The spec §1/§2 ≥80% savings claim is an
// aggregate over the full Google API surface (Google Discovery
// catalogues ~270 APIs averaging tens of methods each — order of a
// few thousand operations). The in-tree embedded catalog snapshot
// only carries 17 representative ops, which is far too small to test
// the savings claim honestly; we materialise this target by cloning
// embedded ops under fresh synthetic op_ids until the catalog reaches
// SpecScaleOpsTarget entries. Lowering this number silently inflates
// the published savings percentage and is a regression — bump in
// lockstep with docs/test-matrix.md row 140 if it ever needs to
// change.
const SpecScaleOpsTarget = 1800

// SpecScaleNaiveCatalog returns a catalog that represents the spec
// §2 "naive author exposes every Google API op as its own tools/list
// entry" baseline scenario at realistic scale. Used by the in-tree
// release-savings test (bead gum-wqk4) so the ≥80% aggregate gate
// becomes computable against the current 200-fixture sample.
//
// The returned catalog contains:
//
//  1. Every op in base (the embedded catalog snapshot) verbatim.
//  2. One synthetic op per unique op_id observed in fixtureDir that
//     isn't already in base (so the naive baseline pays a realistic
//     per-op registration cost for every op the fixture set actually
//     exercises). Meta-tool op_ids beginning with "gum_" are skipped
//     — they aren't Google API ops a naive server would expose.
//  3. Additional synthetic clones of base ops, with fresh op_ids of
//     the form "<service>.synthetic.<n>", padded until len(Ops)
//     reaches SpecScaleOpsTarget. This pads the surface area to the
//     spec-model scale so the registration-overhead arithmetic
//     reflects the full Google API surface, not a 17-op snapshot.
//
// The synthetic ops are byte-deterministic across runs: cloning is
// done in op_id sort order and synthetic IDs increment monotonically.
func SpecScaleNaiveCatalog(base *catalog.Catalog, fixtureDir string) (*catalog.Catalog, error) {
	if base == nil {
		return nil, fmt.Errorf("bench: SpecScaleNaiveCatalog: nil base catalog")
	}
	if len(base.Ops) == 0 {
		return nil, fmt.Errorf("bench: SpecScaleNaiveCatalog: base catalog has no ops")
	}

	out := *base
	out.Ops = append([]catalog.Op{}, base.Ops...)

	seen := make(map[string]bool, len(out.Ops))
	for _, op := range out.Ops {
		seen[op.OpID] = true
	}

	fixtureOpIDs, err := collectFixtureOpIDs(fixtureDir)
	if err != nil {
		return nil, fmt.Errorf("bench: SpecScaleNaiveCatalog: scan fixtures: %w", err)
	}
	for _, opID := range fixtureOpIDs {
		if strings.HasPrefix(opID, "gum_") || seen[opID] {
			continue
		}
		seen[opID] = true
		out.Ops = append(out.Ops, cloneOpWithID(base.Ops[indexForFixtureOp(base.Ops, opID)], opID))
	}

	// Pad with synthetic clones of base ops until we hit the target.
	// Clones are emitted in deterministic round-robin order over the
	// (sorted) base op slice.
	pad := append([]catalog.Op{}, base.Ops...)
	sort.Slice(pad, func(i, j int) bool { return pad[i].OpID < pad[j].OpID })
	n := 0
	for len(out.Ops) < SpecScaleOpsTarget {
		src := pad[n%len(pad)]
		syntheticID := fmt.Sprintf("%s.synthetic.%d", src.OpID, n/len(pad))
		if !seen[syntheticID] {
			seen[syntheticID] = true
			out.Ops = append(out.Ops, cloneOpWithID(src, syntheticID))
		}
		n++
	}

	return &out, nil
}

// indexForFixtureOp returns an index into base.Ops to clone from when
// synthesizing an entry for a fixture op_id. The choice is purely
// for token-realism: ops in similar service families (gmail/drive/
// calendar) tend to have similar schema shapes, so clone from a base
// op in the same service family when possible.
func indexForFixtureOp(ops []catalog.Op, opID string) int {
	prefix := opID
	if i := strings.Index(opID, "."); i > 0 {
		prefix = opID[:i]
	}
	for i, op := range ops {
		if strings.HasPrefix(op.OpID, prefix+".") {
			return i
		}
	}
	return 0
}

// cloneOpWithID returns a deep copy of src with OpID/DefaultVariantID/
// Variant IDs rewritten to use newID as the base. The clone preserves
// every other field so the synthesized tools/list entry pays the same
// per-op token cost as a real Google API op of similar shape.
func cloneOpWithID(src catalog.Op, newID string) catalog.Op {
	dst := src
	dst.OpID = newID
	dst.Title = "Synthetic op " + newID
	dst.DefaultVariantID = newID + ".default"
	dst.Variants = make([]catalog.Variant, len(src.Variants))
	for i, v := range src.Variants {
		v.VariantID = fmt.Sprintf("%s.variant.%d", newID, i)
		dst.Variants[i] = v
	}
	if len(dst.Variants) > 0 {
		dst.DefaultVariantID = dst.Variants[0].VariantID
	}
	dst.ParamsRequired = append([][]string(nil), src.ParamsRequired...)
	dst.ParamsOptional = append([][]string(nil), src.ParamsOptional...)
	dst.Tags = append([]string(nil), src.Tags...)
	dst.DeprecatedOpIDs = append([]string(nil), src.DeprecatedOpIDs...)
	dst.DeprecatedVariantIDs = append([]string(nil), src.DeprecatedVariantIDs...)
	return dst
}

// collectFixtureOpIDs walks fixtureDir and returns the sorted unique
// set of op_ids referenced by request.json files. Order is sorted so
// the resulting naive catalog has byte-deterministic content.
func collectFixtureOpIDs(fixtureDir string) ([]string, error) {
	seen := map[string]bool{}
	err := filepath.WalkDir(fixtureDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "request.json") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var req struct {
			OpID string `json:"op_id"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return nil
		}
		if req.OpID != "" {
			seen[req.OpID] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}
