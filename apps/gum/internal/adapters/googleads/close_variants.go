package googleads

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/ehmo/gum/internal/dispatch"
)

// Google merges close variants of the submitted keywords into one result keyed
// by its own normalized text, so a request with 245 keywords can answer with
// 243 results (gum-o84f). Nothing in the response says which submitted string a
// block answers: two inputs read as one, and a submitted term whose text is not
// in the response reads as zero volume when it is not.
const methodHistorical = "generateKeywordHistoricalMetrics"

// AnnotateResponse maps the submitted keywords onto the results that carry
// their metrics. It adds `matchedInputs` to a result only when the mapping is
// not the obvious one-to-one case, and `unmatchedInputs` only when a submitted
// keyword reached no result, so an ordinary batch pays nothing.
//
// It runs on the shaping path, so `--format raw` still returns the upstream
// bytes (dispatch.ResponseAnnotator). The returned paths say which fields raw
// therefore costs the caller, for the shaping notice to name.
func (a *Adapter) AnnotateResponse(inv *dispatch.Invocation, rv *dispatch.ResolvedVariant, body []byte) ([]byte, []string) {
	if inv == nil || rv == nil || rv.Variant == nil || rv.Variant.Binding == nil || rv.Variant.Binding.HTTP == nil {
		return body, nil
	}
	if customMethod(rv.Variant.Binding.HTTP.Path) != methodHistorical {
		return body, nil
	}

	inputs := stringSliceArg(inv.Args, "keywords")
	if len(inputs) == 0 {
		return body, nil
	}

	// UseNumber keeps every upstream number in its original text, so a body we
	// re-marshal carries the same digits it arrived with.
	doc := map[string]any{}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return body, nil
	}
	results, _ := doc["results"].([]any)
	if len(results) == 0 {
		return body, nil
	}

	matched, unmatched := matchKeywordInputs(inputs, results)
	taggedResults := false
	for i, inputsForResult := range matched {
		res, ok := results[i].(map[string]any)
		if !ok || !needsMatchedInputs(res, inputsForResult) {
			continue
		}

		res["matchedInputs"] = inputsForResult
		taggedResults = true
	}
	if len(unmatched) > 0 {
		doc["unmatchedInputs"] = unmatched
	}

	// One path per field however many results carry it, which is the dot-path
	// convention ShapedResponse.DroppedPaths uses.
	var added []string
	if taggedResults {
		added = append(added, "results.matchedInputs")
	}
	if len(unmatched) > 0 {
		added = append(added, "unmatchedInputs")
	}
	if len(added) == 0 {
		return body, nil
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return body, nil
	}
	return out, added
}

// matchKeywordInputs assigns every submitted keyword to at most one result. It
// returns the inputs per result index and the inputs that reached no result.
func matchKeywordInputs(inputs []string, results []any) ([][]string, []string) {
	exact := map[string]int{}
	folded := map[string]int{}
	for i, r := range results {
		res, ok := r.(map[string]any)
		if !ok {
			continue
		}
		// closeVariants lists the submitted texts Google merged into this
		// result, so it is the mapping the caller is missing.
		keys := append([]string{resultText(res)}, closeVariants(res)...)
		for _, k := range keys {
			if k == "" {
				continue
			}
			claimKey(exact, normalizeKeyword(k), i)
			claimKey(folded, foldKeyword(k), i)
		}
	}

	matched := make([][]string, len(results))
	var unmatched []string
	for _, in := range inputs {
		idx, ok := exact[normalizeKeyword(in)]
		if !ok {
			// Folding closes the punctuation gap that makes
			// "akhal teke horse" and "akhal-teke horse" one result.
			idx, ok = folded[foldKeyword(in)]
		}
		if !ok {
			unmatched = append(unmatched, in)
			continue
		}

		matched[idx] = append(matched[idx], in)
	}
	return matched, unmatched
}

// needsMatchedInputs reports whether the mapping for one result tells the
// caller anything its `text` does not already say.
func needsMatchedInputs(res map[string]any, inputs []string) bool {
	if len(inputs) == 0 {
		return false
	}
	if len(inputs) == 1 && normalizeKeyword(inputs[0]) == normalizeKeyword(resultText(res)) {
		return false
	}
	return true
}

// claimKey records the first result that answers for a key. A later result
// claiming the same key would move an input off the block that named it first.
func claimKey(index map[string]int, key string, i int) {
	if key == "" {
		return
	}
	if _, taken := index[key]; taken {
		return
	}
	index[key] = i
}

func resultText(res map[string]any) string {
	s, _ := res["text"].(string)
	return s
}

func closeVariants(res map[string]any) []string {
	raw, ok := res["closeVariants"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// normalizeKeyword folds case and runs of whitespace, which Google Ads ignores
// in a keyword. Everything else is kept, so two distinct terms cannot collide.
func normalizeKeyword(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// foldKeyword also drops punctuation, which is how a hyphenated term and its
// spaced form arrive as one result.
func foldKeyword(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
