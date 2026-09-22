package main

import (
	"testing"

	"github.com/ehmo/gum/internal/embedded"
)

// setCatalogBlob replaces the embedded catalog for one test and clears the
// session snapshot around it.
//
// loadCatalog reads the session snapshot first, and any test that executes
// the root command publishes one through PersistentPreRunE. Without the
// reset, "no catalog available" would depend on which tests ran before this
// one. Production has one command per process, so the global has no such
// ordering to inherit.
func setCatalogBlob(t *testing.T, blob []byte) {
	t.Helper()

	saved := embedded.CatalogJSON
	savedSnapshot := sessionSnapshot.Load()
	t.Cleanup(func() {
		embedded.CatalogJSON = saved
		sessionSnapshot.Store(savedSnapshot)
	})
	embedded.CatalogJSON = blob
	sessionSnapshot.Store(nil)
}

// TestLoadCatalogEmptyEmbedReturnsNil pins the early-return branch: if the
// embedded catalog blob is empty (a missing or stripped binary asset), the
// helper must surface nil rather than calling json.Unmarshal on []byte{}.
func TestLoadCatalogEmptyEmbedReturnsNil(t *testing.T) {
	setCatalogBlob(t, nil)

	if got := loadCatalog(); got != nil {
		t.Errorf("loadCatalog()=%+v; want nil on empty embed", got)
	}
}

// TestLoadCatalogUnparseableEmbedReturnsNil pins the unmarshal-error
// branch: a corrupt or non-JSON blob must collapse to nil so the CLI
// degrades to "no catalog" rather than panicking on the parse.
func TestLoadCatalogUnparseableEmbedReturnsNil(t *testing.T) {
	setCatalogBlob(t, []byte("{not valid json"))

	if got := loadCatalog(); got != nil {
		t.Errorf("loadCatalog()=%+v; want nil on parse error", got)
	}
}

// TestLoadCatalogPrefersSessionSnapshot pins the precedence gum-26nz added:
// once initSessionCatalog publishes a merged snapshot, every caller reads it
// instead of re-parsing the embedded blob.
func TestLoadCatalogPrefersSessionSnapshot(t *testing.T) {
	setCatalogBlob(t, []byte(`{"catalog_schema_version":1,"ops":[]}`))

	if got := loadCatalog(); got == nil || len(got.Ops) != 0 {
		t.Fatalf("loadCatalog()=%+v; want the embedded fallback", got)
	}

	initSessionCatalog(nil)
	published := sessionSnapshot.Load()
	if published == nil {
		t.Fatal("initSessionCatalog published no snapshot")
	}
	if loadCatalog() != published {
		t.Error("loadCatalog did not return the published session snapshot")
	}
}
