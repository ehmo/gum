package mcp

import (
	"errors"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embed"
)

// TestSearchIndexEmptyCatalogPropagatesBuildErr pins searchIndex's
// `embed.Build err → return nil, err` arm (handlers.go:536-538).
// The handleSearchAPIs caller pre-filters with len(snapshot.Ops)>0, but
// completionRanked calls searchIndex unguarded. A snapshot with no ops
// makes embed.Build return ErrCatalogEmpty; searchIndex MUST propagate
// it verbatim so the caller can degrade rather than mis-cache a nil
// index.
func TestSearchIndexEmptyCatalogPropagatesBuildErr(t *testing.T) {
	t.Parallel()
	s := &Server{snapshot: &catalog.Catalog{
		CatalogSchemaVersion: 1,
		Ops:                  nil,
	}}
	idx, err := s.searchIndex()
	if err == nil {
		t.Fatal("searchIndex(empty-catalog) err=nil; want ErrCatalogEmpty")
	}
	if !errors.Is(err, embed.ErrCatalogEmpty) {
		t.Errorf("err=%v; want errors.Is(err, embed.ErrCatalogEmpty)", err)
	}
	if idx != nil {
		t.Errorf("idx=%v; want nil on err return", idx)
	}
	if s.bm25 != nil {
		t.Errorf("s.bm25=%v after err; want nil (must not cache failed build)", s.bm25)
	}
}
