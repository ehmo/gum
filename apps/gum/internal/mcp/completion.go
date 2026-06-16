package mcp

import (
	"context"
	"sort"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ehmo/gum/internal/embed"
	"github.com/ehmo/gum/internal/help/topics"
)

// completionMaxValues is the wire-budget ceiling for one completion/complete
// reply. The MCP spec recommends bounded result sets so clients can paginate;
// gum's choice of 50 matches the catalog convenience-tool roster size and
// keeps every reply well under 1 KB.
const completionMaxValues = 50

// handleComplete dispatches completion/complete to a focused per-ref source.
// Spec §13 calls for op_id, variant_id, resource-template params, plugin
// names, and closed-enum completions. v0.1.0 wires:
//
//   - ref/resource gum://help/{topic}      → embedded help-topic names
//   - ref/resource gum://op/{id}           → active-snapshot op_ids
//   - ref/resource gum://variant/{id}      → active-snapshot variant_ids
//   - ref/resource gum://plugin/{name}     → profile-inventory plugin names
//   - ref/resource other templates         → empty (handler reserved)
//   - ref/prompt for the static roster     → empty (zero-arg prompts)
//
// Tool-argument completions (gum.code.language, gum.read.format, etc.)
// land alongside the v0.2.0 dispatch-table extension; see known-divergences.
func (s *Server) handleComplete(_ context.Context, req *sdkmcp.CompleteRequest) (*sdkmcp.CompleteResult, error) {
	if req == nil || req.Params == nil || req.Params.Ref == nil {
		return emptyCompleteResult(), nil
	}
	prefix := req.Params.Argument.Value
	argName := req.Params.Argument.Name

	switch req.Params.Ref.Type {
	case "ref/resource":
		return s.completeResourceRef(req.Params.Ref.URI, argName, prefix), nil
	case "ref/prompt":
		return s.completePromptRef(req.Params.Ref.Name, argName, prefix), nil
	default:
		return emptyCompleteResult(), nil
	}
}

// completeResourceRef routes completion requests for resource templates.
// The argument name is the template-variable name (e.g. "topic" for
// gum://help/{topic}); the URI carries the template the client is filling.
func (s *Server) completeResourceRef(uri, argName, prefix string) *sdkmcp.CompleteResult {
	switch {
	case uri == "gum://help/{topic}" && argName == "topic":
		return completionFromSorted(filterByPrefix(topics.Names(), prefix))
	case uri == "gum://op/{id}" && argName == "id":
		return s.completionRanked(filterByPrefix(s.completionOpIDs(), prefix), prefix)
	case uri == "gum://variant/{id}" && argName == "id":
		return completionFromSorted(filterByPrefix(s.completionVariantIDs(), prefix))
	case uri == "gum://plugin/{name}" && argName == "name":
		return completionFromSorted(filterByPrefix(s.completionPluginNames(), prefix))
	default:
		return emptyCompleteResult()
	}
}

// completionOpIDs returns every op_id from the active session catalog
// snapshot. Spec §13 line 3208: completion handlers read the snapshot, never
// the raw BM25 index, so quarantined / pending-restart variants never surface
// as completions. v0.1.0 snapshots contain only first-party ops, so the
// "exclude inactive" branch is a no-op until plugin install paths land.
func (s *Server) completionOpIDs() []string {
	if s.snapshot == nil {
		return nil
	}
	out := make([]string, 0, len(s.snapshot.Ops))
	for i := range s.snapshot.Ops {
		out = append(out, s.snapshot.Ops[i].OpID)
	}
	return out
}

// completionVariantIDs returns every variant_id across every active op.
// The worst-case canary (gum-tsu) seeds a single op with 50 variants to
// exercise the upper bound. Spec §13 line 3259 budgets P95 ≤ 100 ms over a
// catalog with the maximum supported variant fan-out.
func (s *Server) completionVariantIDs() []string {
	if s.snapshot == nil {
		return nil
	}
	out := make([]string, 0, len(s.snapshot.Ops)*2)
	for i := range s.snapshot.Ops {
		for j := range s.snapshot.Ops[i].Variants {
			out = append(out, s.snapshot.Ops[i].Variants[j].VariantID)
		}
	}
	return out
}

// completionPluginNames returns every plugin name visible to MCP from the
// active profile inventory. Spec §13 line 3208 second bullet permits
// inventory-only plugins to appear because gum://plugin/{name} is metadata-
// only. The shared loader already filters installed_pending_restart per
// spec §13 line 3148.
func (s *Server) completionPluginNames() []string {
	rows := s.loadPluginInventoryRows()
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Name)
	}
	return out
}

// completePromptRef routes completion requests for prompt arguments. All
// v0.1.0 prompts are zero-argument so this always returns an empty result;
// the case statement is reserved for the v0.2.0 templated prompts.
func (s *Server) completePromptRef(_ string, _ string, _ string) *sdkmcp.CompleteResult {
	return emptyCompleteResult()
}

// completionRanked applies the spec §13 line 3208 ordering rule for op_id
// completions: exact-prefix match first (case-sensitive — a candidate whose
// literal start matches the user's typed prefix outranks one that only
// matches case-insensitively), BM25 rank second (the same index that
// gum.search_apis consumes), alphabetical as the deterministic tie-breaker.
//
// The candidates have already been filtered to case-insensitive prefix
// matches by the caller. When the BM25 index is unavailable (test
// harness without an embedded catalog), the ranker degrades to alpha sort
// so behaviour is never undefined.
func (s *Server) completionRanked(values []string, prefix string) *sdkmcp.CompleteResult {
	idx, _ := s.searchIndex()
	rankCompletionValues(values, prefix, idx)
	return capCompletionValues(values)
}

// rankCompletionValues sorts values in place per the §13 line 3208 rule.
// Exposed as a package-local function so the ordering invariant is unit-
// testable without standing up a Server.
func rankCompletionValues(values []string, prefix string, idx *embed.Index) {
	bm25Scores := make(map[string]float64, len(values))
	if idx != nil && prefix != "" {
		// Use the typed prefix as a BM25 query. The index reuses op_id tokens
		// (lowercased, dot/space-split, stemmed via tokenize), so a short
		// prefix like "cal" still scores ops whose op_id stems through "cal".
		for _, hit := range idx.Search(prefix, len(values)) {
			bm25Scores[hit.OpID] = hit.Score
		}
	}
	exact := func(v string) bool { return strings.HasPrefix(v, prefix) }
	sort.SliceStable(values, func(i, j int) bool {
		ai, aj := exact(values[i]), exact(values[j])
		if ai != aj {
			return ai
		}
		si, sj := bm25Scores[values[i]], bm25Scores[values[j]]
		if si != sj {
			return si > sj
		}
		return values[i] < values[j]
	})
}

// capCompletionValues caps a pre-sorted slice at completionMaxValues, sets
// hasMore, and wraps it in a CompleteResult. Sort order is preserved.
func capCompletionValues(values []string) *sdkmcp.CompleteResult {
	total := len(values)
	hasMore := total > completionMaxValues
	if hasMore {
		values = values[:completionMaxValues]
	}
	return &sdkmcp.CompleteResult{
		Completion: sdkmcp.CompletionResultDetails{
			Values:  values,
			Total:   total,
			HasMore: hasMore,
		},
	}
}

// completionFromSorted returns a CompleteResult capped at completionMaxValues
// with hasMore set when the cap clipped extra candidates.
func completionFromSorted(values []string) *sdkmcp.CompleteResult {
	sort.Strings(values)
	total := len(values)
	hasMore := total > completionMaxValues
	if hasMore {
		values = values[:completionMaxValues]
	}
	return &sdkmcp.CompleteResult{
		Completion: sdkmcp.CompletionResultDetails{
			Values:  values,
			Total:   total,
			HasMore: hasMore,
		},
	}
}

// emptyCompleteResult is the deterministic empty reply. Always Values:[],
// never nil — clients should never see a JSON null for completion.values.
func emptyCompleteResult() *sdkmcp.CompleteResult {
	return &sdkmcp.CompleteResult{
		Completion: sdkmcp.CompletionResultDetails{Values: []string{}},
	}
}

// filterByPrefix returns the entries of all whose lower-cased value starts
// with the lower-cased prefix. An empty prefix returns a copy of all.
func filterByPrefix(all []string, prefix string) []string {
	if prefix == "" {
		return append([]string(nil), all...)
	}
	lp := strings.ToLower(prefix)
	out := make([]string, 0, len(all))
	for _, v := range all {
		if strings.HasPrefix(strings.ToLower(v), lp) {
			out = append(out, v)
		}
	}
	return out
}
