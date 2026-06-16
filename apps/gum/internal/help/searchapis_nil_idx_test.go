package help_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/help"
)

// TestSearchNilIndexReturnsSearchIndexNotLoaded pins SearchAPIs.Search's
// `s.idx == nil → SEARCH_INDEX_NOT_LOADED` arm (searchapis.go:25-27).
// Reached when the handler was constructed before the BM25 snapshot
// loaded (fresh-process startup or catalog re-load mid-flight). The
// error code is stable so callers can distinguish "not loaded yet" from
// "no results".
func TestSearchNilIndexReturnsSearchIndexNotLoaded(t *testing.T) {
	s := help.NewSearchAPIs(nil)
	results, err := s.Search("anything", 5)
	if err == nil {
		t.Fatalf("Search(nil idx) err=nil; want SEARCH_INDEX_NOT_LOADED")
	}
	if !strings.Contains(err.Error(), "SEARCH_INDEX_NOT_LOADED") {
		t.Errorf("err=%q; want SEARCH_INDEX_NOT_LOADED", err.Error())
	}
	if results != nil {
		t.Errorf("results=%+v; want nil on nil-idx err path", results)
	}
}
