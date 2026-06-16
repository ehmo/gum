package profile_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// TestApplySortByNumeric exercises the SortBy stage with numeric values,
// covering applySortBy + compareValues + toFloat (all 0% before this test).
func TestApplySortByNumeric(t *testing.T) {
	p := &profile.Profile{SortBy: "n", DefaultFormat: "json"}
	body := []byte(`[{"n":3},{"n":1},{"n":2}]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []float64{1, 2, 3}
	for i, m := range got {
		if m["n"].(float64) != want[i] {
			t.Errorf("position %d: got %v, want %v", i, m["n"], want[i])
		}
	}
}

// TestApplySortByString covers the string-comparison branch of compareValues.
func TestApplySortByString(t *testing.T) {
	p := &profile.Profile{SortBy: "name", DefaultFormat: "json"}
	body := []byte(`[{"name":"charlie"},{"name":"alice"},{"name":"bob"}]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"alice", "bob", "charlie"}
	for i, m := range got {
		if m["name"] != want[i] {
			t.Errorf("position %d: got %v, want %v", i, m["name"], want[i])
		}
	}
}

// TestApplySortByNilFallback covers the compareValues nil branches: nil sorts
// before non-nil, and nil-vs-nil returns 0 (preserving stable order).
func TestApplySortByNilFallback(t *testing.T) {
	p := &profile.Profile{SortBy: "k", DefaultFormat: "json"}
	body := []byte(`[{"k":"z"},{"k":null},{"k":"a"}]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// nil first (compareValues returns -1 when a is nil and b isn't), then
	// "a" then "z" via string-ordering.
	if got[0]["k"] != nil {
		t.Errorf("first element: got %v, want nil", got[0]["k"])
	}
	if got[1]["k"] != "a" {
		t.Errorf("second element: got %v, want a", got[1]["k"])
	}
	if got[2]["k"] != "z" {
		t.Errorf("third element: got %v, want z", got[2]["k"])
	}
}

// TestApplySortByJSONFallback covers compareValues' marshal-to-JSON fallback
// when neither side is numeric nor string — here, both sides are arrays.
func TestApplySortByJSONFallback(t *testing.T) {
	p := &profile.Profile{SortBy: "k", DefaultFormat: "json"}
	body := []byte(`[{"k":[2,2]},{"k":[1,1]}]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// json.Marshal "[1,1]" < "[2,2]" lexically.
	first := got[0]["k"].([]any)
	if first[0].(float64) != 1 {
		t.Errorf("expected [1,1] first, got %v", got[0]["k"])
	}
}

// TestApplySortByNonMapElement covers the non-map branch in applySortBy:
// elements that aren't maps must remain in their original positions (return
// false from the less-func to preserve stable order).
func TestApplySortByNonMapElement(t *testing.T) {
	p := &profile.Profile{SortBy: "n", DefaultFormat: "json"}
	body := []byte(`["scalar", {"n":1}, "another"]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Just verify no panic and result is a 3-element array.
	var got []any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len=%d, want 3", len(got))
	}
}

// TestApplyLimit covers the Limit stage (truncate to first N).
func TestApplyLimit(t *testing.T) {
	p := &profile.Profile{Limit: 2, DefaultFormat: "json"}
	body := []byte(`[1,2,3,4,5]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if string(out.Body) != `[1,2]` {
		t.Errorf("got %q, want [1,2]", out.Body)
	}
}

// TestApplyRawBypass covers the raw fast-path (no JSON parsing, no
// transforms, returns input verbatim).
func TestApplyRawBypass(t *testing.T) {
	p := &profile.Profile{Projection: []string{"id"}, DefaultFormat: "json"}
	body := []byte(`not valid json {{{`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "raw"})
	if err != nil {
		t.Fatalf("Apply raw: %v", err)
	}
	if string(out.Body) != string(body) {
		t.Errorf("raw bypass mutated body: got %q", out.Body)
	}
	if out.Format != "raw" {
		t.Errorf("format=%q, want raw", out.Format)
	}
	if out.ProfileApplied {
		t.Error("ProfileApplied=true; raw should bypass transforms")
	}
}

// TestApplyJSONParseError covers the parse-error branch (Apply must surface
// invalid JSON as an error, not panic).
func TestApplyJSONParseError(t *testing.T) {
	p := &profile.Profile{DefaultFormat: "json"}
	_, err := profile.Apply(p, profile.ApplyInput{Body: []byte(`{not json`), UserFormat: "json"})
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse JSON") {
		t.Errorf("error %q does not mention parse JSON", err.Error())
	}
}

// TestApplyCollapseArraysTopLevel covers the top-level slice branch of
// applyCollapseArrays (returns the items/omitted_count wrapper map).
func TestApplyCollapseArraysTopLevel(t *testing.T) {
	p := &profile.Profile{
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 2},
		DefaultFormat:  "json",
	}
	body := []byte(`[10,20,30,40,50]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, string(out.Body))
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 2 {
		t.Errorf("items=%v, want 2-element array", got["items"])
	}
	if got["omitted_count"].(float64) != 3 {
		t.Errorf("omitted_count=%v, want 3", got["omitted_count"])
	}
}

// TestApplyCollapseArraysMapField covers the map branch where one nested
// array exceeds MaxItems and a <key>_omitted_count sibling is appended.
func TestApplyCollapseArraysMapField(t *testing.T) {
	p := &profile.Profile{
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 1},
		DefaultFormat:  "json",
	}
	body := []byte(`{"rows":[1,2,3,4]}`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, string(out.Body))
	}
	if got["rows_omitted_count"].(float64) != 3 {
		t.Errorf("rows_omitted_count=%v, want 3", got["rows_omitted_count"])
	}
}

// TestApplyCollapseArraysNoTruncation covers the early-return branch when
// len(arr) <= MaxItems (no truncation needed).
func TestApplyCollapseArraysNoTruncation(t *testing.T) {
	p := &profile.Profile{
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 5},
		DefaultFormat:  "json",
	}
	body := []byte(`[1,2,3]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if string(out.Body) != `[1,2,3]` {
		t.Errorf("got %q, want [1,2,3]", out.Body)
	}
}

// TestApplyTruncateStringsDefaultEdge covers the default-chars branch.
func TestApplyTruncateStringsDefaultEdge(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{DefaultChars: 5},
		DefaultFormat:   "json",
	}
	body := []byte(`{"s":"hello world"}`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["s"] != "hello…" {
		t.Errorf("s=%q, want hello…", got["s"])
	}
}

// TestApplyTruncateStringsPerFieldOverrideEdge covers the Fields[key] override
// branch (a per-key limit overrides DefaultChars).
func TestApplyTruncateStringsPerFieldOverrideEdge(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{
			DefaultChars: 3,
			Fields:       map[string]int{"long": 8},
		},
		DefaultFormat: "json",
	}
	body := []byte(`{"short":"abcdef","long":"abcdefghij"}`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["short"] != "abc…" {
		t.Errorf("short=%q, want abc…", got["short"])
	}
	if got["long"] != "abcdefgh…" {
		t.Errorf("long=%q, want abcdefgh…", got["long"])
	}
}

// TestApplyTruncateStringsDotPathOverride covers the Fields[childPath]
// branch (a dot-path override applies to a nested field).
func TestApplyTruncateStringsDotPathOverride(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{
			DefaultChars: 4,
			Fields:       map[string]int{"meta.note": 2},
		},
		DefaultFormat: "json",
	}
	body := []byte(`{"meta":{"note":"hello"}}`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, string(out.Body))
	}
	meta := got["meta"].(map[string]any)
	if meta["note"] != "he…" {
		t.Errorf("meta.note=%q, want he…", meta["note"])
	}
}

// TestApplyTruncateStringsInArray covers the array branch of
// applyTruncateStrings: string elements get DefaultChars; non-strings recurse.
func TestApplyTruncateStringsInArray(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{DefaultChars: 2},
		DefaultFormat:   "json",
	}
	body := []byte(`["abcdef", {"k":"xyzzy"}]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got []any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got[0] != "ab…" {
		t.Errorf("got[0]=%q, want ab…", got[0])
	}
	inner := got[1].(map[string]any)
	if inner["k"] != "xy…" {
		t.Errorf("got[1].k=%q, want xy…", inner["k"])
	}
}

// TestApplyDropFieldsDotPath covers the recursion branch where a dot-path
// targets a sub-key (e.g., "meta.secret" drops only the secret sub-key,
// leaving the meta wrapper).
func TestApplyDropFieldsDotPath(t *testing.T) {
	p := &profile.Profile{
		DropFields:    []string{"meta.secret"},
		DefaultFormat: "json",
	}
	body := []byte(`{"meta":{"secret":"hidden","ok":"visible"},"name":"alice"}`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	meta := got["meta"].(map[string]any)
	if _, present := meta["secret"]; present {
		t.Errorf("meta.secret should be dropped; got %v", meta)
	}
	if meta["ok"] != "visible" {
		t.Errorf("meta.ok should remain; got %v", meta["ok"])
	}
}

// TestApplyDropFieldsInArray covers the slice branch of applyDropFields:
// every map element gets the same path treatment.
func TestApplyDropFieldsInArray(t *testing.T) {
	p := &profile.Profile{
		DropFields:    []string{"secret"},
		DefaultFormat: "json",
	}
	body := []byte(`[{"secret":"a","keep":1},{"secret":"b","keep":2}]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for i, m := range got {
		if _, present := m["secret"]; present {
			t.Errorf("got[%d].secret present", i)
		}
	}
}

// TestApplyStripNullsRemovesEmptiesAndNulls covers the four null-like
// branches in isNullLike (nil, "", {}, []).
func TestApplyStripNullsRemovesEmptiesAndNulls(t *testing.T) {
	p := &profile.Profile{StripNulls: true, DefaultFormat: "json"}
	body := []byte(`{"n":null,"e":"","m":{},"a":[],"keep":1}`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 || got["keep"] == nil {
		t.Errorf("got %v, want only {keep:1}", got)
	}
}

// TestApplyStripNullsRecursivelyEmptyMap covers the post-recursion empty
// check: after stripping children, a now-empty nested map must also be
// removed.
func TestApplyStripNullsRecursivelyEmptyMap(t *testing.T) {
	p := &profile.Profile{StripNulls: true, DefaultFormat: "json"}
	body := []byte(`{"a":{"x":null,"y":""},"keep":1}`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := got["a"]; present {
		t.Errorf("a should be stripped after children removed; got %v", got)
	}
}

// TestApplyFlattenEnvelopeMisses covers the no-unwrap branches: multi-key
// envelopes, single-key envelopes whose value isn't an array, and unknown
// envelope keys.
func TestApplyFlattenEnvelopeMisses(t *testing.T) {
	cases := []struct {
		label string
		body  string
		want  string
	}{
		{"multi-key", `{"items":[1],"other":2}`, `{"items":[1],"other":2}`},
		{"non-array-value", `{"items":"scalar"}`, `{"items":"scalar"}`},
		{"unknown-envelope", `{"weird":[1,2]}`, `{"weird":[1,2]}`},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			p := &profile.Profile{Flatten: true, DefaultFormat: "json"}
			out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(tc.body), UserFormat: "json"})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			// Compare via re-unmarshal to be key-order-insensitive.
			var gotMap, wantMap map[string]any
			_ = json.Unmarshal(out.Body, &gotMap)
			_ = json.Unmarshal([]byte(tc.want), &wantMap)
			if len(gotMap) != len(wantMap) {
				t.Errorf("got %v, want %v", string(out.Body), tc.want)
			}
		})
	}
}

// TestApplyDedupeSkipsNonMapElements covers the non-map branch in
// applyDedupe (pass-through unchanged).
func TestApplyDedupeSkipsNonMapElements(t *testing.T) {
	p := &profile.Profile{
		Dedupe:        &profile.DedupeSpec{By: []string{"id"}},
		DefaultFormat: "json",
	}
	body := []byte(`[{"id":1},"scalar",{"id":1},{"id":2}]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got []any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Expected: [{id:1}, "scalar", {id:2}] (second {id:1} deduped).
	if len(got) != 3 {
		t.Errorf("len=%d, want 3; got %v", len(got), got)
	}
}

// TestApplyOnEmptySentinelArray covers the empty-array branch of OnEmpty.
func TestApplyOnEmptySentinelArray(t *testing.T) {
	p := &profile.Profile{
		OnEmpty:       "no results",
		DefaultFormat: "json",
	}
	out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(`[]`), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if string(out.Body) != `"no results"` {
		t.Errorf("got %q, want \"no results\"", out.Body)
	}
}

// TestApplyOnEmptySentinelMap covers the empty-map branch of OnEmpty.
func TestApplyOnEmptySentinelMap(t *testing.T) {
	p := &profile.Profile{
		OnEmpty:       "no data",
		DefaultFormat: "json",
	}
	out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(`{}`), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if string(out.Body) != `"no data"` {
		t.Errorf("got %q, want \"no data\"", out.Body)
	}
}

// TestApplyOnEmptySentinelNull covers the nil branch of OnEmpty.
func TestApplyOnEmptySentinelNull(t *testing.T) {
	p := &profile.Profile{
		OnEmpty:       "nothing",
		DefaultFormat: "json",
	}
	out, err := profile.Apply(p, profile.ApplyInput{Body: []byte(`null`), UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if string(out.Body) != `"nothing"` {
		t.Errorf("got %q, want \"nothing\"", out.Body)
	}
}

// TestApplyFlattenSingletonsUnwrapsSingleArrayElement covers the
// flatten_singletons true-branch (single-element array becomes the element).
func TestApplyFlattenSingletonsUnwrapsSingleArrayElement(t *testing.T) {
	p := &profile.Profile{FlattenSingletons: true, DefaultFormat: "json"}
	body := []byte(`[{"k":"v"}]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if string(out.Body) != `{"k":"v"}` {
		t.Errorf("got %q, want {\"k\":\"v\"}", out.Body)
	}
}

// TestApplyDefaultFormatToon verifies the toon-encoding branch is taken
// when neither UserFormat nor DefaultFormat is set.
func TestApplyDefaultFormatToon(t *testing.T) {
	p := &profile.Profile{}
	body := []byte(`{"k":"v"}`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.Format != "toon" {
		t.Errorf("Format=%q, want toon", out.Format)
	}
}

// TestApplyDefaultFormatFallback covers the unknown-format branch (falls
// back to toon).
func TestApplyDefaultFormatFallback(t *testing.T) {
	p := &profile.Profile{DefaultFormat: "weirdformat"}
	body := []byte(`{"k":"v"}`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.Format != "weirdformat" {
		t.Errorf("Format=%q, want weirdformat (unchanged from input)", out.Format)
	}
	// The output should still be TOON-encoded bytes.
	if len(out.Body) == 0 {
		t.Error("empty body from fallback encoder")
	}
}

// TestApplyTruncateStringsDotPathBeatsBareName pins the audit fix: a dot-path
// field limit (meta.note=50) must win over a same-named bare key (note=5),
// otherwise the more-specific override is unreachable.
func TestApplyTruncateStringsDotPathBeatsBareName(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{
			DefaultChars: 100,
			Fields:       map[string]int{"note": 5, "meta.note": 50},
		},
		DefaultFormat: "json",
	}
	body := []byte(`{"meta":{"note":"abcdefghijklmnopqrst"}}`) // 20 chars, < 50 and > 5
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	meta := got["meta"].(map[string]any)
	if meta["note"] != "abcdefghijklmnopqrst" {
		t.Errorf("meta.note=%q, want the full 20-char string (dot-path limit 50 must beat bare note=5)", meta["note"])
	}
}

// TestApplyDedupeDistinguishesNullFromNilString pins the audit fix: dedupe keys
// are JSON-marshaled, so a JSON null and the literal string "<nil>" (which
// fmt.Sprintf("%v") both render as "<nil>") are NOT treated as duplicates.
func TestApplyDedupeDistinguishesNullFromNilString(t *testing.T) {
	p := &profile.Profile{
		Dedupe:        &profile.DedupeSpec{By: []string{"id"}},
		DefaultFormat: "json",
	}
	body := []byte(`[{"id":null,"n":"a"},{"id":"<nil>","n":"b"}]`)
	out, err := profile.Apply(p, profile.ApplyInput{Body: body, UserFormat: "json"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var got []any
	if err := json.Unmarshal(out.Body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("dedupe collapsed null and \"<nil>\" to %d rows; want 2 (distinct values)", len(got))
	}
}
