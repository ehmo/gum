package embed_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embed"
	"github.com/ehmo/gum/internal/embedded"
)

// TestUnofficialPluginOpsAppearInSearch — gum-76q acceptance gate. After
// building the BM25 index over the embedded catalog snapshot, a search for
// each unofficial-API plugin's distinctive keyword surfaces the corresponding
// op_id within the default top-K. This is the user-visible piece of the
// bead's acceptance: "ops appear in search results".
//
// The four plugins (Scholar, Patents, YouTube Transcripts, Trends) are
// catalog-only stubs in v0.1.0 — the subprocess binaries ship in v0.2.0.
// Until then, gum.search_apis is the discovery surface that lets callers
// know the ops exist and which adapter_key will dispatch them.
func TestUnofficialPluginOpsAppearInSearch(t *testing.T) {
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	idx, err := embed.Build(&cat)
	if err != nil {
		t.Fatalf("embed.Build: %v", err)
	}

	cases := []struct {
		query string
		opID  string
	}{
		{"scholar", "scholar.search"},
		{"patents", "patents.search"},
		{"youtube transcript", "youtube.transcripts.get"},
		{"trends daily", "trends.daily"},
	}

	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			results := idx.Search(c.query, 10)
			if len(results) == 0 {
				t.Fatalf("BM25 search %q returned 0 hits; want at least one containing %q", c.query, c.opID)
			}
			var found bool
			var hits []string
			for _, r := range results {
				hits = append(hits, r.OpID)
				if r.OpID == c.opID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("BM25 search %q did not surface %q in top-10 hits; got %s",
					c.query, c.opID, strings.Join(hits, ", "))
			}
		})
	}
}

// TestUnofficialPluginOpsAreIndexed — sanity check that the four bead-mandated
// op_ids exist in the BM25 index at all (independent of any specific query).
// Fails if the embedded catalog regenerate omits one of them.
func TestUnofficialPluginOpsAreIndexed(t *testing.T) {
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	idx, err := embed.Build(&cat)
	if err != nil {
		t.Fatalf("embed.Build: %v", err)
	}

	indexed := make(map[string]bool, len(idx.OpIDs()))
	for _, id := range idx.OpIDs() {
		indexed[id] = true
	}

	for _, want := range []string{
		"scholar.search",
		"patents.search",
		"youtube.transcripts.get",
		"trends.daily",
	} {
		if !indexed[want] {
			t.Errorf("op %q not present in BM25 index; check internal/embedded/catalog.json", want)
		}
	}
}
