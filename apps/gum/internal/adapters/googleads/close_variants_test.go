package googleads

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/dispatch"
)

// gum-o84f: a 245-keyword request came back with 243 results because Google
// merged close variants, and nothing said which submitted string each block
// answered.

func annotate(t *testing.T, keywords []any, body string) map[string]any {
	t.Helper()
	inv := &dispatch.Invocation{
		OpID: "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		Args: map[string]any{"keywords": keywords},
	}
	out, _ := (&Adapter{}).AnnotateResponse(inv, rvFor(methodHistorical), []byte(body))
	doc := map[string]any{}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("annotated body is not JSON: %v (%s)", err, out)
	}
	return doc
}

func resultAt(t *testing.T, doc map[string]any, i int) map[string]any {
	t.Helper()
	results, ok := doc["results"].([]any)
	if !ok || len(results) <= i {
		t.Fatalf("results[%d] missing: %#v", i, doc)
	}
	res, ok := results[i].(map[string]any)
	if !ok {
		t.Fatalf("results[%d] is not an object: %#v", i, results[i])
	}
	return res
}

func inputsOf(t *testing.T, res map[string]any) []string {
	t.Helper()
	raw, ok := res["matchedInputs"].([]any)
	if !ok {
		t.Fatalf("matchedInputs missing from %#v", res)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("matchedInputs holds a non-string: %#v", v)
		}
		out = append(out, s)
	}
	return out
}

// TestMergedVariantsNameTheirInputs is acceptance criterion (a): both submitted
// strings resolve to the single returned metric block.
func TestMergedVariantsNameTheirInputs(t *testing.T) {
	body := `{"results":[{"text":"akhal-teke horse","closeVariants":["akhal teke horse"],` +
		`"keywordMetrics":{"avgMonthlySearches":"33100"}}]}`

	doc := annotate(t, []any{"akhal-teke horse", "akhal teke horse"}, body)

	results, _ := doc["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %d; the annotation must not add or remove results", len(results))
	}
	want := []string{"akhal-teke horse", "akhal teke horse"}
	if got := inputsOf(t, resultAt(t, doc, 0)); !reflect.DeepEqual(got, want) {
		t.Errorf("matchedInputs = %#v; want %#v", got, want)
	}
	if _, ok := doc["unmatchedInputs"]; ok {
		t.Errorf("unmatchedInputs set although both inputs resolved: %#v", doc["unmatchedInputs"])
	}
}

// TestMergeMatchesWithoutCloseVariants: Google's own normalized text need not
// equal any submitted string, and the punctuation fold is what closes the gap
// when closeVariants does not echo the input.
func TestMergeMatchesWithoutCloseVariants(t *testing.T) {
	body := `{"results":[{"text":"akhal-teke horse","keywordMetrics":{"avgMonthlySearches":"33100"}}]}`

	doc := annotate(t, []any{"akhal teke horse"}, body)

	want := []string{"akhal teke horse"}
	if got := inputsOf(t, resultAt(t, doc, 0)); !reflect.DeepEqual(got, want) {
		t.Errorf("matchedInputs = %#v; want %#v", got, want)
	}
}

// TestOneToOneBatchIsUnchanged: the ordinary batch pays nothing, and the body
// stays byte-identical so an unshaped consumer sees no difference.
func TestOneToOneBatchIsUnchanged(t *testing.T) {
	body := `{"results":[{"text":"horse breeds","keywordMetrics":{"avgMonthlySearches":"63875"}},` +
		`{"text":"horse insurance","keywordMetrics":{"avgMonthlySearches":"2400"}}]}`

	inv := &dispatch.Invocation{
		OpID: "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		Args: map[string]any{"keywords": []any{"Horse Breeds", "horse insurance"}},
	}
	out, _ := (&Adapter{}).AnnotateResponse(inv, rvFor(methodHistorical), []byte(body))
	if string(out) != body {
		t.Errorf("body = %s; want it byte-identical when every input matches its own result", out)
	}
}

// TestUnmatchedInputNamed is the false-zero case: a submitted term that reached
// no result must be named, not read as zero volume.
func TestUnmatchedInputNamed(t *testing.T) {
	body := `{"results":[{"text":"trail riding","keywordMetrics":{"avgMonthlySearches":"18100"}}]}`

	doc := annotate(t, []any{"trail riding", "sailing lessons"}, body)

	raw, ok := doc["unmatchedInputs"].([]any)
	if !ok || len(raw) != 1 || raw[0] != "sailing lessons" {
		t.Errorf("unmatchedInputs = %#v; want [sailing lessons]", doc["unmatchedInputs"])
	}
	if _, ok := resultAt(t, doc, 0)["matchedInputs"]; ok {
		t.Errorf("matchedInputs set on a one-to-one result: %#v", resultAt(t, doc, 0))
	}
}

// TestCloseVariantsMatchesMergedSynonym: the response's own closeVariants entry
// carries the submitted text Google merged, so it resolves even when folding
// cannot (trail rides → trail riding).
func TestCloseVariantsMatchesMergedSynonym(t *testing.T) {
	body := `{"results":[{"text":"trail riding","closeVariants":["trail rides"],` +
		`"keywordMetrics":{"avgMonthlySearches":"18100"}}]}`

	doc := annotate(t, []any{"trail riding", "trail rides"}, body)

	want := []string{"trail riding", "trail rides"}
	if got := inputsOf(t, resultAt(t, doc, 0)); !reflect.DeepEqual(got, want) {
		t.Errorf("matchedInputs = %#v; want %#v", got, want)
	}
}

// TestAnnotateKeepsUpstreamNumbers: a re-marshalled body must carry the same
// digits it arrived with, so large integers do not come back in exponent form.
func TestAnnotateKeepsUpstreamNumbers(t *testing.T) {
	body := `{"results":[{"text":"horse breeds","closeVariants":["horse breed"],` +
		`"keywordMetrics":{"competitionIndex":1234567890123,"avgMonthlySearches":"63875"}}]}`

	doc := annotate(t, []any{"horse breeds", "horse breed"}, body)

	metrics, _ := resultAt(t, doc, 0)["keywordMetrics"].(map[string]any)
	if got, ok := metrics["competitionIndex"].(float64); !ok || got != 1234567890123 {
		t.Errorf("competitionIndex = %#v; want 1234567890123", metrics["competitionIndex"])
	}
	out, _ := (&Adapter{}).AnnotateResponse(&dispatch.Invocation{
		Args: map[string]any{"keywords": []any{"horse breeds", "horse breed"}},
	}, rvFor(methodHistorical), []byte(body))
	if !strings.Contains(string(out), "1234567890123") {
		t.Errorf("annotated body = %s; want the literal digits preserved", out)
	}
}

// TestAnnotateIgnoresOtherMethods: only the historical-metrics batch merges
// close variants, so no other method may be touched.
func TestAnnotateIgnoresOtherMethods(t *testing.T) {
	body := `{"results":[{"text":"mars cruise"}]}`
	for _, method := range []string{"generateKeywordIdeas", "generateKeywordForecastMetrics", "search"} {
		inv := &dispatch.Invocation{Args: map[string]any{"keywords": []any{"mars cruises"}}}
		out, _ := (&Adapter{}).AnnotateResponse(inv, rvFor(method), []byte(body))
		if string(out) != body {
			t.Errorf("%s: body = %s; want it unchanged", method, out)
		}
	}
}

// TestAnnotateToleratesOddBodies: an unreadable or empty response is still a
// correct response, so the annotator returns it as it stands.
func TestAnnotateToleratesOddBodies(t *testing.T) {
	cases := []string{`not json`, `{}`, `{"results":[]}`, `{"results":"nope"}`, ``}
	for _, body := range cases {
		inv := &dispatch.Invocation{Args: map[string]any{"keywords": []any{"horse breeds"}}}
		out, _ := (&Adapter{}).AnnotateResponse(inv, rvFor(methodHistorical), []byte(body))
		if string(out) != body {
			t.Errorf("body %q became %q; want it unchanged", body, out)
		}
	}
}

// TestAnnotateNeedsKeywords: without the request keywords there is nothing to
// map, so the body is untouched.
func TestAnnotateNeedsKeywords(t *testing.T) {
	body := `{"results":[{"text":"horse breeds"}]}`
	out, _ := (&Adapter{}).AnnotateResponse(&dispatch.Invocation{Args: map[string]any{}}, rvFor(methodHistorical), []byte(body))
	if string(out) != body {
		t.Errorf("body = %s; want it unchanged", out)
	}
}

// TestAnnotateNilInputs: a missing invocation or variant must not panic.
func TestAnnotateNilInputs(t *testing.T) {
	body := []byte(`{"results":[]}`)
	if got, _ := (&Adapter{}).AnnotateResponse(nil, rvFor(methodHistorical), body); string(got) != string(body) {
		t.Errorf("nil invocation: body = %s", got)
	}
	if got, _ := (&Adapter{}).AnnotateResponse(&dispatch.Invocation{}, nil, body); string(got) != string(body) {
		t.Errorf("nil variant: body = %s", got)
	}
}

// TestDefaultedTargetingReachesTheWire proves the shape the profile defaults
// hand the adapter (gum-0nn2) is the shape the adapter sends: a []string of
// bare geo ids and a bare language id both gain their resource prefix.
func TestDefaultedTargetingReachesTheWire(t *testing.T) {
	srv, _, gotBody := captureServer(t, `{"results":[{"text":"horse breeds","keywordMetrics":{"avgMonthlySearches":"63875"}}]}`)

	adapter := NewAdapter(func() string { return "DEV" })
	adapter.BaseURL = srv.URL

	inv := &dispatch.Invocation{
		OpID: "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		Args: map[string]any{
			"customerId":         "1234567890",
			"keywords":           []any{"horse breeds"},
			"geoTargetConstants": []string{"2840"},
			"language":           "1000",
		},
	}
	if _, err := adapter.Execute(t.Context(), inv, rvFor(methodHistorical), &dispatch.Credentials{Token: "T"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	geo, _ := (*gotBody)["geoTargetConstants"].([]any)
	if len(geo) != 1 || geo[0] != "geoTargetConstants/2840" {
		t.Errorf("geoTargetConstants = %#v; want [geoTargetConstants/2840]", (*gotBody)["geoTargetConstants"])
	}
	if (*gotBody)["language"] != "languageConstants/1000" {
		t.Errorf("language = %#v; want languageConstants/1000", (*gotBody)["language"])
	}
}

// gum-9l5c: the shaping notice offers `--format raw` to recover a dropped
// field, and raw bypasses this annotator. The second return value is what lets
// the notice say which fields that costs, so it must name exactly the fields
// this call added.
func TestAnnotateResponseReportsTheFieldsItAdded(t *testing.T) {
	merged := `{"results":[{"text":"akhal teke horse","closeVariants":["akhal-teke horse"],` +
		`"keywordMetrics":{"avgMonthlySearches":"4400"}}]}`

	inv := &dispatch.Invocation{
		OpID: "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		Args: map[string]any{"keywords": []any{"akhal teke horse", "akhal-teke horse", "sailing lessons"}},
	}
	out, paths := (&Adapter{}).AnnotateResponse(inv, rvFor(methodHistorical), []byte(merged))

	want := []string{"results.matchedInputs", "unmatchedInputs"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %#v; want %#v", paths, want)
	}

	// Every reported path must be in the body it returned, or the notice names
	// a field the caller cannot find.
	doc := map[string]any{}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("annotated body is not JSON: %v (%s)", err, out)
	}
	if _, ok := doc["unmatchedInputs"]; !ok {
		t.Errorf("unmatchedInputs reported but absent: %s", out)
	}
	if _, ok := resultAt(t, doc, 0)["matchedInputs"]; !ok {
		t.Errorf("results.matchedInputs reported but absent: %s", out)
	}
}

// Only unmatchedInputs: the singular arm the notice's noun agreement needs.
func TestAnnotateResponseReportsUnmatchedInputsAlone(t *testing.T) {
	body := `{"results":[{"text":"trail riding","keywordMetrics":{"avgMonthlySearches":"18100"}}]}`

	inv := &dispatch.Invocation{
		OpID: "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		Args: map[string]any{"keywords": []any{"trail riding", "sailing lessons"}},
	}
	if _, paths := (&Adapter{}).AnnotateResponse(inv, rvFor(methodHistorical), []byte(body)); !reflect.DeepEqual(paths, []string{"unmatchedInputs"}) {
		t.Errorf("paths = %#v; want [unmatchedInputs]", paths)
	}
}

// An untouched body reports nothing, so the ordinary batch keeps the plain raw
// hint.
func TestAnnotateResponseReportsNoPathsWhenUnchanged(t *testing.T) {
	body := `{"results":[{"text":"horse breeds","keywordMetrics":{"avgMonthlySearches":"63875"}}]}`

	inv := &dispatch.Invocation{
		OpID: "googleads.keywordPlanIdeas.generateKeywordHistoricalMetrics",
		Args: map[string]any{"keywords": []any{"horse breeds"}},
	}
	out, paths := (&Adapter{}).AnnotateResponse(inv, rvFor(methodHistorical), []byte(body))
	if len(paths) != 0 {
		t.Errorf("paths = %#v; want none for a one-to-one batch", paths)
	}
	if string(out) != body {
		t.Errorf("body = %s; want it byte-identical", out)
	}
}
