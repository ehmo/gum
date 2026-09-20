package dispatch

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ehmo/gum/internal/catalog"
	"github.com/ehmo/gum/internal/output/profile"
)

// historicalBody builds a generateKeywordHistoricalMetrics response shaped like
// the gum-pmbp repro: n results, each carrying the matchedInputs annotation the
// adapter adds and the closeVariants field the builtin profile drops.
func historicalBody(t *testing.T, n int) []byte {
	t.Helper()
	results := make([]any, 0, n)
	for i := range n {
		results = append(results, map[string]any{
			"text": fmt.Sprintf("kw-%d", i),
			"keywordMetrics": map[string]any{
				"avgMonthlySearches": "100",
				"competition":        "LOW",
			},
			"matchedInputs": []any{fmt.Sprintf("kw-%d", i), fmt.Sprintf("kw-%d-variant", i)},
			"closeVariants": []any{fmt.Sprintf("kw-%d close", i)},
		})
	}

	body, err := json.Marshal(map[string]any{"results": results})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return body
}

// historicalVariant returns the dispatch inputs for the builtin Google Ads
// historical-metrics profile, whose collapse_arrays cap is 100.
func historicalVariant(t *testing.T) *profile.Profile {
	t.Helper()
	p, ok := profile.BuiltinLookup("googleads.keyword_historical.v1")
	if !ok {
		t.Fatal("builtin googleads.keyword_historical.v1 missing")
	}
	if p.CollapseArrays == nil || p.CollapseArrays.MaxItems != 100 {
		t.Fatalf("profile cap = %+v; want max_items 100", p.CollapseArrays)
	}
	return p
}

// resultsOf decodes a shaped JSON body and returns its results array.
func resultsOf(t *testing.T, body []byte) ([]any, map[string]any) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal shaped body: %v\n%s", err, body)
	}
	rows, ok := decoded["results"].([]any)
	if !ok {
		t.Fatalf("results missing or not an array: %v", decoded)
	}
	return rows, decoded
}

// TestShapeResponseMaxItemsOverridesBuiltinCap is the gum-pmbp acceptance test
// at the dispatch seam. The builtin profile caps at 100 results; a 245-keyword
// batch returns 243. Without an override the caller sees 100 rows and a count
// of 143 omitted. With the override every row and every matchedInput survives.
func TestShapeResponseMaxItemsOverridesBuiltinCap(t *testing.T) {
	const total = 243
	prof := historicalVariant(t)
	body := historicalBody(t, total)
	rv := &ResolvedVariant{Variant: &catalog.Variant{}}
	d := &dispatcher{}

	t.Run("profile cap applies by default", func(t *testing.T) {
		inv := &Invocation{OpID: "googleads.x", Format: "json", OutputProfile: prof}
		out, err := d.shapeResponse(t.Context(), inv, rv, &Response{Body: body})
		if err != nil {
			t.Fatalf("shapeResponse: %v", err)
		}

		rows, decoded := resultsOf(t, out.Body)
		if len(rows) != 100 {
			t.Errorf("results = %d; want 100", len(rows))
		}
		if got := decoded["results_omitted_count"]; got != float64(143) {
			t.Errorf("results_omitted_count = %v; want 143", got)
		}
		if len(out.CollapsedArrays) != 1 {
			t.Fatalf("CollapsedArrays = %+v; want one entry", out.CollapsedArrays)
		}
		if c := out.CollapsedArrays[0]; c.Kept != 100 || c.Omitted != 143 || c.CountKey != "results_omitted_count" {
			t.Errorf("collapse record = %+v; want kept 100 omitted 143 key results_omitted_count", c)
		}
	})

	t.Run("max-items all returns every result", func(t *testing.T) {
		inv := &Invocation{
			OpID:          "googleads.x",
			Format:        "json",
			OutputProfile: prof,
			MaxItems:      profile.MaxItemsOverride{Mode: profile.MaxItemsUnlimited},
		}
		out, err := d.shapeResponse(t.Context(), inv, rv, &Response{Body: body})
		if err != nil {
			t.Fatalf("shapeResponse: %v", err)
		}

		rows, decoded := resultsOf(t, out.Body)
		if len(rows) != total {
			t.Fatalf("results = %d; want %d", len(rows), total)
		}
		if _, present := decoded["results_omitted_count"]; present {
			t.Error("results_omitted_count present although nothing was omitted")
		}
		if len(out.CollapsedArrays) != 0 {
			t.Errorf("CollapsedArrays = %+v; want none", out.CollapsedArrays)
		}

		// Every submitted keyword must be reachable through matchedInputs.
		seen := map[string]bool{}
		for _, row := range rows {
			obj, ok := row.(map[string]any)
			if !ok {
				t.Fatalf("row is %T; want object", row)
			}
			inputs, ok := obj["matchedInputs"].([]any)
			if !ok {
				t.Fatalf("matchedInputs missing on row %v", obj["text"])
			}
			for _, in := range inputs {
				seen[fmt.Sprint(in)] = true
			}
		}
		if len(seen) != total*2 {
			t.Errorf("matchedInputs covered %d values; want %d", len(seen), total*2)
		}
		for i := range total {
			for _, want := range []string{fmt.Sprintf("kw-%d", i), fmt.Sprintf("kw-%d-variant", i)} {
				if !seen[want] {
					t.Fatalf("submitted keyword %q unreachable through matchedInputs", want)
				}
			}
		}
	})

	t.Run("max-items lowers the cap", func(t *testing.T) {
		inv := &Invocation{
			OpID:          "googleads.x",
			Format:        "json",
			OutputProfile: prof,
			MaxItems:      profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 5},
		}
		out, err := d.shapeResponse(t.Context(), inv, rv, &Response{Body: body})
		if err != nil {
			t.Fatalf("shapeResponse: %v", err)
		}

		rows, decoded := resultsOf(t, out.Body)
		if len(rows) != 5 {
			t.Errorf("results = %d; want 5", len(rows))
		}
		if got := decoded["results_omitted_count"]; got != float64(total-5) {
			t.Errorf("results_omitted_count = %v; want %d", got, total-5)
		}
	})

	t.Run("raw ignores the override and stays identity", func(t *testing.T) {
		inv := &Invocation{
			OpID:          "googleads.x",
			Format:        "raw",
			OutputProfile: prof,
			MaxItems:      profile.MaxItemsOverride{Mode: profile.MaxItemsLimit, Value: 5},
		}
		out, err := d.shapeResponse(t.Context(), inv, rv, &Response{Body: body})
		if err != nil {
			t.Fatalf("shapeResponse: %v", err)
		}
		if string(out.Body) != string(body) {
			t.Error("raw body mutated; raw must stay a byte-identical passthrough")
		}
	})
}
