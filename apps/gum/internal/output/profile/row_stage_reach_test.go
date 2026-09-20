// Package profile — regression tests for the stage-7 row stages.
//
// Defect: dedupe, sort_by and limit were each gated on the top-level value
// being a []any. Almost every Google list response is an object
// ({"messages":[...],"nextPageToken":"..."}), and stage 5 wraps a top-level
// array into {"items":[...],"omitted_count":N}, so all three stages were
// silently skipped for real bodies. A second defect sat inside dedupe: a `by`
// field absent from every row produced the same null key for every row, so the
// whole result set collapsed to a single row.
package profile

import (
	"encoding/json"
	"testing"
)

func TestDedupeRunsOnObjectBodyRecordArray(t *testing.T) {
	const body = `{"messages":[{"id":"a"},{"id":"a"},{"id":"b"}],"nextPageToken":"t"}`
	out, err := Apply(&Profile{Dedupe: &DedupeSpec{By: []string{"id"}}},
		ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got struct {
		Messages      []map[string]any `json:"messages"`
		NextPageToken string           `json:"nextPageToken"`
	}
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal output: %v; body=%s", err, out.Body)
	}
	if len(got.Messages) != 2 {
		t.Errorf("messages has %d rows; want 2 (dedupe did not run on the record array): %s",
			len(got.Messages), out.Body)
	}
	if got.NextPageToken != "t" {
		t.Errorf("nextPageToken = %q; want \"t\" (siblings must survive)", got.NextPageToken)
	}
}

func TestSortByRunsOnObjectBodyRecordArray(t *testing.T) {
	const body = `{"files":[{"n":3},{"n":1},{"n":2}]}`
	out, err := Apply(&Profile{SortBy: "n"}, ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := `{"files":[{"n":1},{"n":2},{"n":3}]}`
	if got := string(out.Body); got != want {
		t.Errorf("sort_by did not reach the record array\n got: %s\nwant: %s", got, want)
	}
}

func TestLimitRunsAfterCollapseWrap(t *testing.T) {
	const body = `[{"n":1},{"n":2},{"n":3},{"n":4},{"n":5}]`
	p := &Profile{
		CollapseArrays: &CollapseArraysSpec{MaxItems: 4},
		Limit:          2,
	}
	out, err := Apply(p, ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal output: %v; body=%s", err, out.Body)
	}
	if len(got.Items) != 2 {
		t.Errorf("items has %d rows; want 2 (limit did not run after the stage-5 wrap): %s",
			len(got.Items), out.Body)
	}
}

// TestDedupeKeepsRowsMissingEveryKeyField is the data-loss case: every row keyed
// on an absent field hashed to the same null key, so 3 distinct rows became 1.
func TestDedupeKeepsRowsMissingEveryKeyField(t *testing.T) {
	const body = `[{"a":1},{"a":2},{"a":3}]`
	out, err := Apply(&Profile{Dedupe: &DedupeSpec{By: []string{"notAField"}}},
		ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Body, &rows); err != nil {
		t.Fatalf("unmarshal output: %v; body=%s", err, out.Body)
	}
	if len(rows) != 3 {
		t.Errorf("dedupe on an absent field kept %d of 3 rows: %s", len(rows), out.Body)
	}
}

// TestDedupeStillCollapsesPartialKeyMatch guards the fix: a row that carries at
// least one of the `by` fields is still keyed and still deduplicated.
func TestDedupeStillCollapsesPartialKeyMatch(t *testing.T) {
	const body = `[{"id":"a","v":1},{"id":"a","v":2},{"id":"b"}]`
	out, err := Apply(&Profile{Dedupe: &DedupeSpec{By: []string{"id", "missing"}}},
		ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Body, &rows); err != nil {
		t.Fatalf("unmarshal output: %v; body=%s", err, out.Body)
	}
	if len(rows) != 2 {
		t.Errorf("dedupe kept %d rows; want 2 (id=a collapses, id=b survives): %s",
			len(rows), out.Body)
	}
}

// TestRowStagesSkipAmbiguousObjectBody documents the conservative arm: two
// array-valued keys give no single record array, so the row stages leave the
// body alone rather than guessing.
func TestRowStagesSkipAmbiguousObjectBody(t *testing.T) {
	const body = `{"a":[{"id":1},{"id":1}],"b":[{"id":2},{"id":2}]}`
	out, err := Apply(&Profile{Dedupe: &DedupeSpec{By: []string{"id"}}},
		ApplyInput{Body: []byte(body), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := string(out.Body); got != body {
		t.Errorf("ambiguous body was modified\n got: %s\nwant: %s", got, body)
	}
}
