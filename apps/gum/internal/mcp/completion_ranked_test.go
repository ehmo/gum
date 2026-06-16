package mcp

import (
	"encoding/json"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/embed"
	"github.com/ehmo/gum/internal/embedded"
)

// TestRankCompletionValuesExactPrefixWinsOverCaseInsensitive — gum-eul8
// acceptance: spec §13 line 3208 sorts completion results "by exact-prefix
// match first and BM25 rank second". A value whose case-sensitive start
// matches the typed prefix outranks one whose lower-cased start matches.
// (filterByPrefix is case-insensitive so both candidates pass the gate.)
func TestRankCompletionValuesExactPrefixWinsOverCaseInsensitive(t *testing.T) {
	values := []string{"Gmail.Users.List", "gmail.users.list"}
	rankCompletionValues(values, "gmail", nil)

	if values[0] != "gmail.users.list" {
		t.Errorf("ranked[0] = %q; want gmail.users.list (case-sensitive exact-prefix match wins)", values[0])
	}
	if values[1] != "Gmail.Users.List" {
		t.Errorf("ranked[1] = %q; want Gmail.Users.List (case-insensitive match drops to lower tier)", values[1])
	}
}

// TestRankCompletionValuesBM25BreaksTiesBetweenExactPrefixCandidates —
// when multiple candidates have the same exact-prefix outcome, BM25 score
// over the embedded catalog decides. The op whose document is more
// relevant to the prefix sorts higher.
func TestRankCompletionValuesBM25BreaksTiesBetweenExactPrefixCandidates(t *testing.T) {
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	idx, err := embed.Build(&cat)
	if err != nil {
		t.Fatalf("embed.Build: %v", err)
	}

	// Both candidates pass exact-prefix on "scholar"; only scholar.search
	// is in the index. The indexed op must come out ahead.
	values := []string{"scholar.unindexed.fake", "scholar.search"}
	rankCompletionValues(values, "scholar", idx)
	if values[0] != "scholar.search" {
		t.Errorf("ranked[0] = %q; want scholar.search (BM25-indexed op outranks the fake)", values[0])
	}
}

// TestRankCompletionValuesAlphaTieBreak — when BM25 scores tie (e.g. neither
// op is in the BM25 index, or the prefix is empty), order is alphabetical.
// This is the deterministic last-resort fallback so identical inputs always
// produce identical client output.
func TestRankCompletionValuesAlphaTieBreak(t *testing.T) {
	values := []string{"foo.zeta", "foo.alpha", "foo.mike"}
	rankCompletionValues(values, "foo", nil)
	want := []string{"foo.alpha", "foo.mike", "foo.zeta"}
	for i := range want {
		if values[i] != want[i] {
			t.Errorf("ranked[%d] = %q; want %q (alpha tie-break)", i, values[i], want[i])
			break
		}
	}
}

// TestRankCompletionValuesNilIndexFallsBackToAlpha — when the search index
// has not yet been built (NewServer was called without embedded catalog),
// rankCompletionValues must not panic and must still produce a deterministic
// ordering. Alpha-sort is the contract.
func TestRankCompletionValuesNilIndexFallsBackToAlpha(t *testing.T) {
	values := []string{"b", "a", "c"}
	rankCompletionValues(values, "", nil)
	want := []string{"a", "b", "c"}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("ranked = %v; want %v", values, want)
		}
	}
}

// TestRankCompletionValuesEmptyPrefixSkipsBM25 — with no prefix typed, the
// caller wants all candidates back; BM25 cannot score a zero-token query
// usefully, so the ranker must skip the BM25 lookup and fall through to
// alpha. Verifies the prefix-empty guard inside rankCompletionValues.
func TestRankCompletionValuesEmptyPrefixSkipsBM25(t *testing.T) {
	var cat catalog.Catalog
	if err := json.Unmarshal(embedded.CatalogJSON, &cat); err != nil {
		t.Fatalf("unmarshal embedded catalog: %v", err)
	}
	idx, err := embed.Build(&cat)
	if err != nil {
		t.Fatalf("embed.Build: %v", err)
	}
	values := []string{"zzz.op", "aaa.op"}
	rankCompletionValues(values, "", idx)
	if values[0] != "aaa.op" {
		t.Errorf("ranked[0] = %q; want aaa.op (empty-prefix → alpha sort)", values[0])
	}
}
