package profile_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// batchResponse builds a generateKeywordHistoricalMetrics body with n results.
// Google merges near-duplicate keywords, so the adapter's matchedInputs names
// the submitted keywords behind each merged result: result i carries two
// inputs, "kw-<i>" and "kw-<i>-variant". n results therefore map 2n inputs.
func batchResponse(n int) []byte {
	results := make([]any, 0, n)
	for i := range n {
		results = append(results, map[string]any{
			"text": fmt.Sprintf("kw-%d", i),
			"keywordMetrics": map[string]any{
				"avgMonthlySearches": "100",
				"competition":        "LOW",
			},
			"matchedInputs": []any{fmt.Sprintf("kw-%d", i), fmt.Sprintf("kw-%d-variant", i)},
			"closeVariants": []any{"dropped by the profile"},
		})
	}
	body, err := json.Marshal(map[string]any{"results": results, "unmatchedInputs": []any{}})
	if err != nil {
		panic(err)
	}
	return body
}

// shapedInputs returns every keyword named in any result's matchedInputs.
func shapedInputs(t *testing.T, body []byte) (results int, inputs map[string]bool) {
	t.Helper()
	var got struct {
		Results []struct {
			MatchedInputs []string `json:"matchedInputs"`
		} `json:"results"`
		ResultsOmittedCount int `json:"results_omitted_count"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal shaped body: %v", err)
	}
	inputs = map[string]bool{}
	for _, r := range got.Results {
		for _, in := range r.MatchedInputs {
			inputs[in] = true
		}
	}
	return len(got.Results), inputs
}

// TestMaxItemsOverrideReachesEveryMatchedInput is the gum-pmbp acceptance case.
// 243 merged results carry the 245 submitted keywords (two pairs merged). The
// profile caps at 100, so the default response strands 143 results and the
// inputs behind them; the override has to return all of them in a format that
// still carries matchedInputs.
func TestMaxItemsOverrideReachesEveryMatchedInput(t *testing.T) {
	p, ok := profile.BuiltinLookup("googleads.keyword_historical.v1")
	if !ok {
		t.Fatal("googleads.keyword_historical.v1 not embedded")
	}
	body := batchResponse(243)

	capped, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply (profile cap): %v", err)
	}
	gotResults, _ := shapedInputs(t, capped.Body)
	if gotResults != 100 {
		t.Fatalf("profile cap returned %d results; want the shipped cap of 100", gotResults)
	}

	for _, tc := range []struct {
		name string
		over profile.MaxItemsOverride
	}{
		{"all", profile.MaxItemsOverride{Mode: profile.MaxItemsUnlimited}},
		{"explicit higher cap", profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 500}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json", MaxItems: tc.over})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			results, inputs := shapedInputs(t, out.Body)
			if results != 243 {
				t.Fatalf("results = %d; want 243", results)
			}
			if len(inputs) != 486 {
				t.Fatalf("matchedInputs covered %d keywords; want 486", len(inputs))
			}
			for i := range 243 {
				for _, want := range []string{fmt.Sprintf("kw-%d", i), fmt.Sprintf("kw-%d-variant", i)} {
					if !inputs[want] {
						t.Fatalf("submitted keyword %q appears in no result's matchedInputs", want)
					}
				}
			}
			if len(out.CollapsedArrays) != 0 {
				t.Errorf("CollapsedArrays = %v; want empty when nothing was truncated", out.CollapsedArrays)
			}
			if strings.Contains(string(out.Body), "results_omitted_count") {
				t.Error("shaped body still carries results_omitted_count")
			}
		})
	}
}

// TestMaxItemsOverrideLowersTheCap: the override replaces the profile's cap in
// both directions, not only upward.
func TestMaxItemsOverrideLowersTheCap(t *testing.T) {
	p, _ := profile.BuiltinLookup("googleads.keyword_historical.v1")
	out, err := profile.Apply(p, profile.ApplyInput{
		Body:       batchResponse(243),
		UserFormat: "json",
		MaxItems:   profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 5},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	results, _ := shapedInputs(t, out.Body)
	if results != 5 {
		t.Fatalf("results = %d; want 5", results)
	}
	if len(out.CollapsedArrays) != 1 {
		t.Fatalf("CollapsedArrays = %v; want one entry", out.CollapsedArrays)
	}
	got := out.CollapsedArrays[0]
	want := profile.CollapsedArray{Field: "results", CountKey: "results_omitted_count", Kept: 5, Omitted: 238}
	if got != want {
		t.Errorf("CollapsedArrays[0] = %+v; want %+v", got, want)
	}
}

// TestCollapseReportsOmittedResults pins the gum-pmbp notice criterion: the
// stderr note must state the omitted-result count and name results_omitted_count,
// not only the removed field.
func TestCollapseReportsOmittedResults(t *testing.T) {
	p, _ := profile.BuiltinLookup("googleads.keyword_historical.v1")
	out, err := profile.Apply(p, profile.ApplyInput{Body: batchResponse(243), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	notice := profile.ShapingNotice(profile.NoticeInput{
		DroppedPaths:    out.DroppedPaths,
		CollapsedArrays: out.CollapsedArrays,
		RawHint:         "--format raw",
		MaxItemsHint:    "--max-items all",
	})
	for _, want := range []string{"143", "243", "results_omitted_count", "--max-items all", "results.closeVariants"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q does not mention %q", notice, want)
		}
	}
	// The omitted rows outrank the removed field: 143 of 243 results is the
	// larger loss, so it has to come first.
	if strings.Index(notice, "143") > strings.Index(notice, "closeVariants") {
		t.Errorf("notice leads with the removed field, not the omitted results: %q", notice)
	}
}

// TestRawIsByteIdenticalPassthrough: --format raw returns the executor body
// verbatim whatever the profile and the override say (gum-pmbp acceptance 4).
func TestRawIsByteIdenticalPassthrough(t *testing.T) {
	p, _ := profile.BuiltinLookup("googleads.keyword_historical.v1")
	body := batchResponse(243)
	for _, over := range []profile.MaxItemsOverride{
		{},
		{Mode: profile.MaxItemsUnlimited},
		{Mode: profile.MaxItemsLimit, Value: 3},
	} {
		out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "raw", MaxItems: over})
		if err != nil {
			t.Fatalf("Apply(raw, %+v): %v", over, err)
		}
		if string(out.Body) != string(body) {
			t.Fatalf("raw body changed under %+v", over)
		}
		if len(out.CollapsedArrays) != 0 {
			t.Errorf("raw pass reported CollapsedArrays = %v", out.CollapsedArrays)
		}
	}
}

// TestMaxItemsOverrideCapsAProfileWithoutOne: a profile that declares no
// collapse_arrays rule still honours the caller's cap.
func TestMaxItemsOverrideCapsAProfileWithoutOne(t *testing.T) {
	out, err := profile.Apply(&profile.Profile{}, profile.ApplyInput{
		Body:       []byte(`{"results":[1,2,3,4,5]}`),
		UserFormat: "json",
		MaxItems:   profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 2},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got struct {
		Results             []int `json:"results"`
		ResultsOmittedCount int   `json:"results_omitted_count"`
	}
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Results) != 2 || got.ResultsOmittedCount != 3 {
		t.Fatalf("results=%v results_omitted_count=%d; want 2 kept and 3 omitted", got.Results, got.ResultsOmittedCount)
	}
}

// TestBareArrayCollapseReportsItems covers the wrapped-array branch: a body
// that is itself an array becomes {items, omitted_count}.
func TestBareArrayCollapseReportsItems(t *testing.T) {
	out, err := profile.Apply(&profile.Profile{}, profile.ApplyInput{
		Body:       []byte(`[1,2,3,4,5]`),
		UserFormat: "json",
		MaxItems:   profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 2},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got struct {
		Items        []int `json:"items"`
		OmittedCount int   `json:"omitted_count"`
	}
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, out.Body)
	}
	if len(got.Items) != 2 || got.OmittedCount != 3 {
		t.Fatalf("items=%v omitted_count=%d; want 2 and 3", got.Items, got.OmittedCount)
	}
	want := profile.CollapsedArray{Field: "", CountKey: "omitted_count", Kept: 2, Omitted: 3}
	if len(out.CollapsedArrays) != 1 || out.CollapsedArrays[0] != want {
		t.Fatalf("CollapsedArrays = %+v; want [%+v]", out.CollapsedArrays, want)
	}
}

// TestCollapseRecordsEverySiblingArray: adding <key>_omitted_count while
// ranging over the same map leaves it unspecified whether the new keys are
// visited, so the collapse collects before it writes. Ten sibling arrays make
// a missed one visible.
func TestCollapseRecordsEverySiblingArray(t *testing.T) {
	body := map[string]any{}
	for i := range 10 {
		body[fmt.Sprintf("arr%d", i)] = []any{1, 2, 3, 4}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := profile.Apply(&profile.Profile{}, profile.ApplyInput{
		Body:       raw,
		UserFormat: "json",
		MaxItems:   profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 1},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(out.CollapsedArrays) != 10 {
		t.Fatalf("CollapsedArrays = %d entries; want 10", len(out.CollapsedArrays))
	}
	for i, c := range out.CollapsedArrays {
		wantField := fmt.Sprintf("arr%d", i)
		if c.Field != wantField {
			t.Fatalf("CollapsedArrays[%d].Field = %q; want %q (sorted by field)", i, c.Field, wantField)
		}
		if c.Kept != 1 || c.Omitted != 3 || c.CountKey != wantField+"_omitted_count" {
			t.Errorf("CollapsedArrays[%d] = %+v", i, c)
		}
	}
	var shaped map[string]any
	if err := json.Unmarshal(out.Body, &shaped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for i := range 10 {
		key := fmt.Sprintf("arr%d_omitted_count", i)
		if shaped[key] == nil {
			t.Errorf("shaped body missing %s", key)
		}
	}
}
