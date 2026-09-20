// Package profile_test — the record array of a body carrying two arrays.
//
// The Google Ads keyword-history adapter adds unmatchedInputs beside results,
// so the shaped body holds two top-level arrays. The record-array heuristic
// gave up on that shape: a 245-keyword call reported result_count 0 with 100
// records in the body, and the row stages skipped it.
package profile_test

import (
	"encoding/json"
	"testing"

	profile "github.com/ehmo/gum/internal/output/profile"
)

// keywordHistoryBody is the shipped googleads.keyword_historical.v1 shape:
// the records under "results", the adapter's unmatched keywords beside them.
func keywordHistoryBody(rows int) []byte {
	doc := map[string]any{
		"results":         make([]any, 0, rows),
		"unmatchedInputs": []any{"unsold keyword"},
	}
	arr := doc["results"].([]any)
	for i := 0; i < rows; i++ {
		arr = append(arr, map[string]any{"text": "kw", "id": i})
	}
	doc["results"] = arr

	b, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return b
}

func TestResultCountReadsResultsBesideASecondArray(t *testing.T) {
	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json"},
		profile.ApplyInput{Body: keywordHistoryBody(3)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.ResultCount != 3 {
		t.Errorf("ResultCount = %d; want 3 (the body carries three records)", out.ResultCount)
	}
	if out.OmittedCount != 0 {
		t.Errorf("OmittedCount = %d; want 0 (nothing was shaped away)", out.OmittedCount)
	}
}

// TestKeywordHistoryCountsMatchTheBody is the gum-36zi repro: 243 keyword
// results capped at 100 reported (0, 143), which spec §9.1 rule 3 reads as
// "results existed but were shaped away" while 100 records sat in the body.
func TestKeywordHistoryCountsMatchTheBody(t *testing.T) {
	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 100}},
		profile.ApplyInput{Body: keywordHistoryBody(243)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.ResultCount != 100 {
		t.Errorf("ResultCount = %d; want 100", out.ResultCount)
	}
	if out.OmittedCount != 143 {
		t.Errorf("OmittedCount = %d; want 143", out.OmittedCount)
	}
}

func TestRowStagesRunOnResultsBesideASecondArray(t *testing.T) {
	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", Limit: 2},
		profile.ApplyInput{Body: keywordHistoryBody(5)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	var got struct {
		Results         []map[string]any `json:"results"`
		UnmatchedInputs []any            `json:"unmatchedInputs"`
	}
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal shaped body: %v; body=%s", err, out.Body)
	}
	if len(got.Results) != 2 {
		t.Errorf("results has %d rows; want 2 (limit skipped the record array): %s", len(got.Results), out.Body)
	}
	if len(got.UnmatchedInputs) != 1 {
		t.Errorf("unmatchedInputs has %d entries; want 1 (the sibling array must survive)", len(got.UnmatchedInputs))
	}
}

// TestTwoUnnamedArraysStillHaveNoRecordArray is the control. Without a named
// key there is no way to tell which array holds the records, and guessing
// would count or reorder the wrong one.
func TestTwoUnnamedArraysStillHaveNoRecordArray(t *testing.T) {
	out, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", Limit: 1},
		profile.ApplyInput{Body: []byte(`{"alpha":[{"id":1},{"id":2}],"beta":[{"id":3},{"id":4}]}`)},
	)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.ResultCount != 0 {
		t.Errorf("ResultCount = %d; want 0 (no record array)", out.ResultCount)
	}

	var got map[string][]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal shaped body: %v", err)
	}
	if len(got["alpha"]) != 2 || len(got["beta"]) != 2 {
		t.Errorf("limit ran on an ambiguous body: %s", out.Body)
	}
}
