// Package profile_test — the §9.1 envelope counts.
//
// Two defects, one data flow: Apply reported lossy from the profile fields
// alone and counted only the omitted totals stage 5 writes into the body.
// Both needed the pre-shaping record count, which Apply threw away at parse
// time.
package profile_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// rowsBody builds {"messages":[{"id":"row-0"},...]} with n rows.
func rowsBody(n int) []byte {
	rows := make([]any, 0, n)
	for i := range n {
		rows = append(rows, map[string]any{"id": fmt.Sprintf("row-%d", i)})
	}
	body, err := json.Marshal(map[string]any{"messages": rows})
	if err != nil {
		panic(err)
	}
	return body
}

// TestLossyFollowsThePerCallMaxItems pins gum-nn5k. A caller's max_items caps
// arrays whether or not the profile declares collapse_arrays, so lossy has to
// describe the pipeline that ran, not the profile that was loaded. The old
// code read the profile alone and answered lossy:false next to
// omitted_count:95.
func TestLossyFollowsThePerCallMaxItems(t *testing.T) {
	p := &profile.Profile{DefaultFormat: "json"}

	out, err := profile.Apply(p, profile.ApplyInput{
		Body:     rowsBody(100),
		MaxItems: profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 5},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !out.Lossy {
		t.Errorf("Lossy = false; want true (the call capped 100 rows at 5)")
	}
	if out.ResultCount != 5 || out.OmittedCount != 95 {
		t.Errorf("counts = (%d,%d); want (5,95)", out.ResultCount, out.OmittedCount)
	}
}

// TestLossyDropsWhenTheCallRemovesTheOnlyCap is the other half of gum-nn5k:
// --max-items all skips stage 5, so a profile whose only lossy stage was the
// cap returns everything upstream sent and must not claim loss.
func TestLossyDropsWhenTheCallRemovesTheOnlyCap(t *testing.T) {
	p := &profile.Profile{
		DefaultFormat:  "json",
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 2},
	}

	out, err := profile.Apply(p, profile.ApplyInput{
		Body:     rowsBody(10),
		MaxItems: profile.MaxItemsOverride{Mode: profile.MaxItemsUnlimited},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.Lossy {
		t.Errorf("Lossy = true; want false (max_items=all skipped the only lossy stage)")
	}
	if out.ResultCount != 10 || out.OmittedCount != 0 {
		t.Errorf("counts = (%d,%d); want (10,0)", out.ResultCount, out.OmittedCount)
	}
}

// TestOmittedCountsRowsLimitRemoved pins gum-h0ft for stage "limit". Only
// collapse_arrays writes an omitted_count sibling, so a profile that cuts 7 of
// 10 rows with limit reported omitted_count:0, which §9.1 rule 3 defines as
// "the upstream API returned zero results".
func TestOmittedCountsRowsLimitRemoved(t *testing.T) {
	p := &profile.Profile{DefaultFormat: "json", Limit: 3}

	out, err := profile.Apply(p, profile.ApplyInput{Body: rowsBody(10)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.ResultCount != 3 || out.OmittedCount != 7 {
		t.Errorf("counts = (%d,%d); want (3,7)", out.ResultCount, out.OmittedCount)
	}
}

// TestOmittedCountsRowsDedupeRemoved pins gum-h0ft for stage "dedupe".
func TestOmittedCountsRowsDedupeRemoved(t *testing.T) {
	p := &profile.Profile{
		DefaultFormat: "json",
		Dedupe:        &profile.DedupeSpec{By: []string{"id"}},
	}
	body := []byte(`{"messages":[{"id":"a"},{"id":"a"},{"id":"b"},{"id":"b"}]}`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.ResultCount != 2 || out.OmittedCount != 2 {
		t.Errorf("counts = (%d,%d); want (2,2)", out.ResultCount, out.OmittedCount)
	}
}

// TestOmittedCountKeepsTheLargerOfTheTwoSources pins gum-h0ft where both
// accounting sources are live: a nested array collapsed inside the one row
// that survives writes a sibling count that the row arithmetic cannot see.
func TestOmittedCountKeepsTheLargerOfTheTwoSources(t *testing.T) {
	p := &profile.Profile{
		DefaultFormat:  "json",
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 2},
	}
	body := []byte(`{"tags":["a","b","c","d","e"]}`)

	out, err := profile.Apply(p, profile.ApplyInput{Body: body})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.ResultCount != 2 || out.OmittedCount != 3 {
		t.Errorf("counts = (%d,%d); want (2,3)", out.ResultCount, out.OmittedCount)
	}
}

// TestEmptyUpstreamAndShapedAwayDifferByCounts is what gum-bk4p is really
// asking for. Both calls carry the same on_empty string, which spec §9.4
// requires on a genuinely empty result as much as on a shaped-away one, so the
// message cannot be the discriminator. The count pair is: §9.1 rule 3 reads
// (0,0) as "the upstream API returned zero results" and (0,N) as "results
// existed but were shaped away". Before the omitted_count fix both cases
// reported (0,0) and the model could not tell them apart.
func TestEmptyUpstreamAndShapedAwayDifferByCounts(t *testing.T) {
	msg := "No matching messages."

	empty, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", OnEmpty: msg, KeepFields: []string{"messages.id"}},
		profile.ApplyInput{Body: []byte(`{"messages":[]}`)},
	)
	if err != nil {
		t.Fatalf("Apply (upstream empty): %v", err)
	}
	if empty.OnEmptyMessage != msg {
		t.Errorf("upstream empty: OnEmptyMessage = %q; want %q", empty.OnEmptyMessage, msg)
	}
	if empty.ResultCount != 0 || empty.OmittedCount != 0 {
		t.Errorf("upstream empty: counts = (%d,%d); want (0,0)", empty.ResultCount, empty.OmittedCount)
	}

	shapedAway, err := profile.Apply(
		&profile.Profile{DefaultFormat: "json", OnEmpty: msg, DropFields: []string{"messages"}},
		profile.ApplyInput{Body: rowsBody(10)},
	)
	if err != nil {
		t.Fatalf("Apply (shaped away): %v", err)
	}
	if shapedAway.OnEmptyMessage != msg {
		t.Errorf("shaped away: OnEmptyMessage = %q; want %q", shapedAway.OnEmptyMessage, msg)
	}
	if shapedAway.ResultCount != 0 || shapedAway.OmittedCount != 10 {
		t.Errorf("shaped away: counts = (%d,%d); want (0,10)", shapedAway.ResultCount, shapedAway.OmittedCount)
	}
}

// TestOnEmptyFiresWhenShapingEmptiesTheBody keeps the §9.1 rule 2 positive
// case: a non-empty upstream that the field filters reduce to nothing.
func TestOnEmptyFiresWhenShapingEmptiesTheBody(t *testing.T) {
	p := &profile.Profile{
		DefaultFormat: "json",
		OnEmpty:       "Nothing left after shaping.",
		DropFields:    []string{"only"},
	}

	out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(`{"only":{"a":1}}`)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.OnEmptyMessage != "Nothing left after shaping." {
		t.Errorf("OnEmptyMessage = %q; want the profile's on_empty string", out.OnEmptyMessage)
	}
}

// TestZeroMaxItemsKeepsItsMessageOnAnEmptyUpstream holds the two rules that
// meet here. Spec §9.1 discriminator 5 requires intentional_zero_max_items on
// every call whose cap is 0, including the one where upstream also returned
// zero, and §13 forbids that flag without a message. The on_empty gate must
// not strip the message out from under the flag.
func TestZeroMaxItemsKeepsItsMessageOnAnEmptyUpstream(t *testing.T) {
	p := &profile.Profile{
		DefaultFormat:  "json",
		OnEmpty:        "Rows collapsed on purpose.",
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 0},
	}

	out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(`{"messages":[]}`)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !out.IntentionalZeroMaxItems {
		t.Error("IntentionalZeroMaxItems = false; want true (the cap in force is 0)")
	}
	if out.OnEmptyMessage != "Rows collapsed on purpose." {
		t.Errorf("OnEmptyMessage = %q; the §13 invariant pairs the flag with a message", out.OnEmptyMessage)
	}
}
