// gum-3ko acceptance tests: BM25 index coverage MUST be a deterministic
// function of the catalog snapshot — no missing ops, no phantom hits.
package embed_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embed"
	"github.com/ehmo/gum/internal/embedded"
)

// snapshotOpSet returns the set of op_ids present in cat.Ops.
func snapshotOpSet(cat *catalog.Catalog) map[string]bool {
	m := make(map[string]bool, len(cat.Ops))
	for _, op := range cat.Ops {
		m[op.OpID] = true
	}
	return m
}

// loadEmbeddedCatalog returns the binary-embedded catalog snapshot used by the
// production server, or skips when the build was compiled without one.
func loadEmbeddedCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	if len(embedded.CatalogJSON) == 0 {
		t.Skip("no embedded catalog in this build (CatalogJSON empty)")
	}
	var c catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &c); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	return &c
}

// TestCatalogIndexSnapshotInvariant asserts that every op in the catalog
// snapshot is represented in the BM25 index, AND that the index contains no
// op that the snapshot does not. As the catalog grows beyond the 17 curated
// v0.1.0 ops, this prevents a silent drift where the index falls behind the
// dispatch surface.
//
// Spec anchors: §5.3 (retrieval index), §5.4 (active session snapshot).
func TestCatalogIndexSnapshotInvariant(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	idx, err := embed.Build(cat)
	if err != nil {
		t.Fatalf("embed.Build: %v", err)
	}

	snapshot := snapshotOpSet(cat)
	indexed := make(map[string]bool, len(snapshot))
	for _, id := range idx.OpIDs() {
		indexed[id] = true
	}

	for id := range snapshot {
		if !indexed[id] {
			t.Errorf("op_id %q present in snapshot but missing from BM25 index", id)
		}
	}
	for id := range indexed {
		if !snapshot[id] {
			t.Errorf("op_id %q present in BM25 index but missing from snapshot (phantom)", id)
		}
	}

	if len(indexed) != len(snapshot) {
		t.Errorf("index covers %d ops; snapshot has %d (set equality required)", len(indexed), len(snapshot))
	}
}

// TestBM25SnapshotSubsetInvariant asserts that every search result's op_id
// is in the catalog snapshot. The retrieval surface MUST never expose a
// non-dispatchable op. We probe with a handful of common tokens drawn from
// the snapshot itself to exercise multiple service families and avoid
// hard-coding op names.
func TestBM25SnapshotSubsetInvariant(t *testing.T) {
	cat := loadEmbeddedCatalog(t)
	idx, err := embed.Build(cat)
	if err != nil {
		t.Fatalf("embed.Build: %v", err)
	}
	snapshot := snapshotOpSet(cat)

	// Build a small set of likely-matching query tokens by mining service
	// prefixes from op_ids ("gmail.users.messages.list" → "gmail").
	prefixes := map[string]bool{}
	for id := range snapshot {
		if i := strings.IndexByte(id, '.'); i > 0 {
			prefixes[id[:i]] = true
		}
	}
	if len(prefixes) == 0 {
		t.Skip("no dotted op_ids in snapshot; cannot derive query tokens")
	}

	for prefix := range prefixes {
		results := idx.Search(prefix, 50)
		for _, r := range results {
			if !snapshot[r.OpID] {
				t.Errorf("search %q returned op_id %q which is NOT in snapshot (subset invariant violated)", prefix, r.OpID)
			}
		}
	}

	// Also probe a stable known-good query that exercises the cross-service
	// summary tokens (lower-case to dodge the analyzer's case fold).
	for _, q := range []string{"list", "send", "create"} {
		for _, r := range idx.Search(q, 25) {
			if !snapshot[r.OpID] {
				t.Errorf("search %q returned phantom op_id %q", q, r.OpID)
			}
		}
	}

	// Cross-check: at least one query MUST return something, otherwise we
	// silently passed by returning no results everywhere.
	probe := ""
	for p := range prefixes {
		probe = p
		break
	}
	if got := idx.Search(probe, 1); len(got) == 0 {
		t.Errorf("baseline search for prefix %q returned zero results; subset invariant trivially holds but index appears empty", probe)
	}
}

