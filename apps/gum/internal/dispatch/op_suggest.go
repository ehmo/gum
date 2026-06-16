package dispatch

import (
	"sort"
	"strings"
)

// suggestOpIDs returns up to limit catalog op_ids closest to a (non-matching)
// opID, ranked by case-insensitive Levenshtein distance. It powers the
// OP_NOT_FOUND "suggestions" detail so a caller who typos an op_id gets a "did
// you mean" hint instead of an always-empty list (gum-l0op #2).
//
// A distance threshold (half the query length, floored at 2) drops candidates
// too dissimilar to be a plausible typo, so a query resembling no op yields an
// empty slice rather than three nonsensical suggestions. The result is always a
// non-nil slice so JSON envelopes never emit "suggestions":null.
func suggestOpIDs(opID string, candidates []string, limit int) []string {
	out := []string{}
	if limit <= 0 || len(candidates) == 0 {
		return out
	}
	q := strings.ToLower(opID)
	maxDist := len(q) / 2
	if maxDist < 2 {
		maxDist = 2
	}

	type scored struct {
		id   string
		dist int
	}
	ranked := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		ranked = append(ranked, scored{id: c, dist: levenshtein(q, strings.ToLower(c))})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].dist != ranked[j].dist {
			return ranked[i].dist < ranked[j].dist
		}
		return ranked[i].id < ranked[j].id // stable, deterministic tie-break
	})

	for _, s := range ranked {
		if len(out) >= limit {
			break
		}
		if s.dist > maxDist {
			continue
		}
		out = append(out, s.id)
	}
	return out
}

// levenshtein computes the edit distance between a and b using the classic
// two-row dynamic-programming table (O(len(a)·len(b)) time, O(len(b)) space).
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = min3(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
