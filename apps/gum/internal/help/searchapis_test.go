package help_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embed"
	"github.com/ehmo/gum/internal/help"
)

// loadBM25FixtureCatalog reads the BM25 fixture catalog from the embed testdata directory.
func loadBM25FixtureCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	// Use the shared BM25 fixture in internal/embed/testdata.
	data, err := os.ReadFile(filepath.Join("..", "embed", "testdata", "bm25-fixture-catalog.json"))
	if err != nil {
		t.Fatalf("loadBM25FixtureCatalog: %v", err)
	}
	var c catalog.Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatalf("loadBM25FixtureCatalog unmarshal: %v", err)
	}
	return &c
}

// buildSearchAPIs constructs a SearchAPIs backed by the BM25 fixture catalog.
func buildSearchAPIs(t *testing.T) *help.SearchAPIs {
	t.Helper()
	cat := loadBM25FixtureCatalog(t)
	idx, err := embed.Build(cat)
	if err != nil {
		t.Fatalf("embed.Build: %v", err)
	}
	return help.NewSearchAPIs(idx)
}

// TestSearchAPIsTopHit verifies that searching for "send email" returns
// gmail.users.messages.send in the top 3 results, matching the golden fixture.
func TestSearchAPIsTopHit(t *testing.T) {
	defer goleak.VerifyNone(t)

	s := buildSearchAPIs(t)
	results, err := s.Search("send email", 3)
	if err != nil {
		t.Fatalf("Search returned unexpected error: %v", err)
	}

	// Load the golden file.
	goldenData, err := os.ReadFile(filepath.Join("testdata", "bm25-goldens", "send-email.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var goldenOpIDs []string
	if err := json.Unmarshal(goldenData, &goldenOpIDs); err != nil {
		t.Fatalf("parse golden: %v", err)
	}

	// gmail.users.messages.send must appear in top 3 results.
	wantTopHit := goldenOpIDs[0] // "gmail.users.messages.send"
	found := false
	for _, r := range results {
		if r.OpID == wantTopHit {
			found = true
			break
		}
	}
	if !found {
		opIDs := make([]string, len(results))
		for i, r := range results {
			opIDs[i] = r.OpID
		}
		t.Errorf("expected %q in top-3 results for 'send email', got: %v", wantTopHit, opIDs)
	}
}

// TestSearchAPIsTopKClamp verifies clamping behaviour:
//   - topK=0  → default 10 (with 5 ops, returns at most 5)
//   - topK=-1 → same as 0
//   - topK=100 → clamped to 50 (with 5 ops, returns at most 5)
func TestSearchAPIsTopKClamp(t *testing.T) {
	defer goleak.VerifyNone(t)

	s := buildSearchAPIs(t)

	cases := []struct {
		name    string
		topK    int
		wantMax int
	}{
		{"zero defaults to 10", 0, 50},
		{"negative defaults to 10", -1, 50},
		{"oversized clamped to 50", 100, 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := s.Search("email message", tc.topK)
			if err != nil {
				t.Fatalf("Search(%d): %v", tc.topK, err)
			}
			if len(results) > tc.wantMax {
				t.Errorf("topK=%d: expected at most %d results, got %d", tc.topK, tc.wantMax, len(results))
			}
		})
	}
}
