// Package help implements gum.search_apis, gum.describe_op, and help-topic surfacing (spec.md §14).
package help

import (
	"errors"

	"github.com/ehmo/gum/internal/embed"
)

// SearchAPIs is the handler for the gum.search_apis meta-tool.
// It wraps a BM25 index and applies the per-profile k/truncation knobs
// defined in spec.md §2.1 and §10.3.
type SearchAPIs struct {
	idx *embed.Index
}

// NewSearchAPIs constructs a SearchAPIs handler backed by the provided BM25 index.
func NewSearchAPIs(idx *embed.Index) *SearchAPIs {
	return &SearchAPIs{idx: idx}
}

// Search invokes BM25 and returns the result struct used by gum.search_apis.
// topK defaults to 10 when 0 or negative, and is clamped to [1, 50].
func (s *SearchAPIs) Search(query string, topK int) ([]embed.SearchResult, error) {
	if s.idx == nil {
		return nil, errors.New("SEARCH_INDEX_NOT_LOADED")
	}
	if topK <= 0 {
		topK = 10
	}
	if topK > 50 {
		topK = 50
	}
	results := s.idx.Search(query, topK)
	return results, nil
}
