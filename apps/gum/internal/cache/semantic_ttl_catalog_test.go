// semantic_ttl_catalog_test.go — the §10.3 per-op TTL table must name ops the
// catalog actually carries.
//
// Spec §10.3 writes the table in op-*type* prose ("gmail.profiles.get:
// 3600s"). Those labels were copied into the map as literal keys, so the lookup
// in TTLForOp never matched and the ops silently inherited the 60s default. A
// dead key costs nothing visible: the cache still works, it just expires an
// immutable reference 60 times an hour.

package cache

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embedded"
)

// catalogOpIDs returns every op_id in the embedded catalog.
func catalogOpIDs(t *testing.T) map[string]bool {
	t.Helper()
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	if len(cat.Ops) == 0 {
		t.Fatal("embedded catalog carries no ops")
	}
	ids := make(map[string]bool, len(cat.Ops))
	for i := range cat.Ops {
		ids[cat.Ops[i].OpID] = true
	}
	return ids
}

// TestPerOpTTLKeysAreRealCatalogOps is the guard against the whole class. A key
// that matches no op is unreachable, and nothing else in the build notices.
func TestPerOpTTLKeysAreRealCatalogOps(t *testing.T) {
	ids := catalogOpIDs(t)
	for opID := range PerOpTTL {
		if !ids[opID] {
			t.Errorf("PerOpTTL[%q] names no catalog op, so the entry is unreachable", opID)
		}
	}
}

// TestPerOpTTLCoversEverySpecTier pins one live op per tier of the
// spec §10.3 table, keyed by the op id the dispatcher passes to Set.
func TestPerOpTTLCoversEverySpecTier(t *testing.T) {
	c := NewSemanticCache(SemanticConfig{})
	want := map[string]time.Duration{
		"calendar.events.list":   60 * time.Second,   // calendar.events: 60s
		"calendar.events.get":    60 * time.Second,   // calendar.events: 60s
		"drive.files.list":       300 * time.Second,  // drive.files.list: 300s
		"gmail.users.getProfile": 3600 * time.Second, // "gmail.profiles.get": 3600s
		"calendar.colors.get":    24 * time.Hour,     // user-immutable references: 24h
	}
	for opID, d := range want {
		if got := c.TTLForOp(opID); got != d {
			t.Errorf("TTLForOp(%q) = %v; want %v", opID, got, d)
		}
	}
}
