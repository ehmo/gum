package dispatch

import (
	"reflect"
	"testing"
)

// TestSuggestOpIDsRanksNearestFirst pins the "did you mean" fuzzy matcher
// (gum-l0op #2): a typo'd op_id must surface the closest real op_id first so
// the OP_NOT_FOUND envelope can carry actionable suggestions instead of an
// always-empty list.
func TestSuggestOpIDsRanksNearestFirst(t *testing.T) {
	candidates := []string{
		"gmail.messages.list",
		"gmail.messages.send",
		"calendar.events.list",
		"drive.files.list",
	}
	got := suggestOpIDs("gmial.messages.list", candidates, 3)
	if len(got) == 0 || got[0] != "gmail.messages.list" {
		t.Fatalf("suggestOpIDs typo => %v; want nearest 'gmail.messages.list' first", got)
	}
	if len(got) > 3 {
		t.Errorf("suggestOpIDs returned %d suggestions; want <= 3", len(got))
	}
}

// TestSuggestOpIDsDropsGarbage pins the threshold: a query that resembles no
// catalog op must yield an empty slice (never nil), so callers get a clean
// "results":[] rather than three nonsensical suggestions.
func TestSuggestOpIDsDropsGarbage(t *testing.T) {
	candidates := []string{
		"gmail.messages.list",
		"calendar.events.list",
	}
	got := suggestOpIDs("zzzzzzzzzzzzzzz", candidates, 3)
	if !reflect.DeepEqual(got, []string{}) {
		t.Errorf("suggestOpIDs(garbage) = %v; want empty non-nil slice", got)
	}
}

// TestSuggestOpIDsEmptyCandidates guards the nil-catalog path.
func TestSuggestOpIDsEmptyCandidates(t *testing.T) {
	got := suggestOpIDs("gmail.messages.list", nil, 3)
	if !reflect.DeepEqual(got, []string{}) {
		t.Errorf("suggestOpIDs(no candidates) = %v; want empty non-nil slice", got)
	}
}
