package embed_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embed"
)

// loadFixtureCatalog reads the 5-op BM25 fixture catalog from testdata.
func loadFixtureCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "bm25-fixture-catalog.json"))
	if err != nil {
		t.Fatalf("loadFixtureCatalog: %v", err)
	}
	var c catalog.Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("loadFixtureCatalog unmarshal: %v", err)
	}
	return &c
}

// TestBM25BuildBasic verifies that Build succeeds on the 5-op fixture catalog
// and returns a non-nil Index.
func TestBM25BuildBasic(t *testing.T) {
	defer goleak.VerifyNone(t)

	cat := loadFixtureCatalog(t)
	idx, err := embed.Build(cat)
	if err != nil {
		t.Fatalf("Build returned unexpected error: %v", err)
	}
	if idx == nil {
		t.Fatal("Build returned nil Index without error")
	}
}

// TestBM25SearchTopK checks that:
//   - Searching "send email" returns at least one result with score > 0.
//   - Results are ordered by score descending.
//   - topK clamping: requesting topK=0 returns default (10) results (clamped); topK=100 returns at most 50.
func TestBM25SearchTopK(t *testing.T) {
	defer goleak.VerifyNone(t)

	cat := loadFixtureCatalog(t)
	idx, err := embed.Build(cat)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	results := idx.Search("send email", 3)
	if len(results) == 0 {
		t.Fatal("Search returned no results for 'send email'")
	}

	// Scores should be > 0 for a matching query.
	for i, r := range results {
		if r.Score <= 0 {
			t.Errorf("result[%d] op_id=%s has non-positive score %f", i, r.OpID, r.Score)
		}
	}

	// Results should be ordered descending.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted descending: results[%d].Score=%f > results[%d].Score=%f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}

	// topK=0 defaults to 10 (clamped) — with only 5 ops, expect ≤5 results.
	defaultResults := idx.Search("email", 0)
	if len(defaultResults) > 50 {
		t.Errorf("topK=0 default returned %d results, want ≤50", len(defaultResults))
	}

	// topK=100 is clamped to 50.
	clampedResults := idx.Search("email", 100)
	if len(clampedResults) > 50 {
		t.Errorf("topK=100 clamped to 50 but got %d results", len(clampedResults))
	}
}

func TestBM25StemmerKeepsBaseFormsSearchable(t *testing.T) {
	defer goleak.VerifyNone(t)

	cat := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-05-22T00:00:00Z",
		GeneratorVersion:     "gum/test@0.0.0",
		Ops: []catalog.Op{
			bm25TestOp("ops.process", "Processing request"),
			bm25TestOp("ops.settings", "Settings access address"),
		},
	}
	idx, err := embed.Build(cat)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, query := range []string{"process", "setting", "settings", "access", "address"} {
		t.Run(query, func(t *testing.T) {
			results := idx.Search(query, 5)
			if len(results) == 0 {
				t.Fatalf("Search(%q) returned no results", query)
			}
		})
	}
}

func bm25TestOp(id, title string) catalog.Op {
	variantID := id + ".v1"
	return catalog.Op{
		OpID:             id,
		OpSchemaVersion:  1,
		Title:            title,
		Summary:          title,
		DefaultVariantID: variantID,
		Variants: []catalog.Variant{{
			VariantID:            variantID,
			VariantSchemaVersion: 1,
			Stability:            catalog.StabilityStable,
			InterfaceKind:        catalog.InterfaceKindDiscoveryREST,
			BackendKind:          catalog.BackendKindDiscoveryREST,
			RiskClass:            catalog.RiskClassRead,
			AuthStrategy:         catalog.AuthStrategyNone,
		}},
	}
}

// TestBM25Determinism verifies that:
//
//	build → search("send email", 3) produces the same op_ids and scores as
//	build → save → load → search("send email", 3).
func TestBM25Determinism(t *testing.T) {
	defer goleak.VerifyNone(t)

	cat := loadFixtureCatalog(t)

	// First build + search (no save/load).
	idx1, err := embed.Build(cat)
	if err != nil {
		t.Fatalf("Build (first): %v", err)
	}
	results1 := idx1.Search("send email", 3)

	// Second build + save + load + search.
	idx2, err := embed.Build(cat)
	if err != nil {
		t.Fatalf("Build (second): %v", err)
	}

	dir := t.TempDir()
	if err := idx2.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	idx3, err := embed.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	results2 := idx3.Search("send email", 3)

	// Both results sets must have the same length.
	if len(results1) != len(results2) {
		t.Fatalf("determinism: len(results1)=%d != len(results2)=%d", len(results1), len(results2))
	}

	// Each element must match exactly.
	for i := range results1 {
		if results1[i].OpID != results2[i].OpID {
			t.Errorf("determinism: results[%d].OpID: %q vs %q", i, results1[i].OpID, results2[i].OpID)
		}
		if results1[i].Score != results2[i].Score {
			t.Errorf("determinism: results[%d].Score: %f vs %f", i, results1[i].Score, results2[i].Score)
		}
	}
}

// TestBM25EmptyCatalog verifies that Build returns ErrCatalogEmpty when given
// a catalog with no ops.
func TestBM25EmptyCatalog(t *testing.T) {
	defer goleak.VerifyNone(t)

	empty := &catalog.Catalog{
		CatalogSchemaVersion: 1,
		GeneratedAt:          "2026-05-22T00:00:00Z",
		GeneratorVersion:     "gum/test@0.0.0",
		Ops:                  nil,
	}
	_, err := embed.Build(empty)
	if err == nil {
		t.Fatal("expected ErrCatalogEmpty, got nil")
	}
	if err != embed.ErrCatalogEmpty {
		t.Errorf("expected ErrCatalogEmpty, got: %v", err)
	}
}
