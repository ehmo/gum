package profile_test

// operators_test.go — RED team tests for expression-profile DSL operators.
// Bead: gum-np38.5 — Harden expression-profile DSL evaluator to full spec §9.1 operator set.
//
// ALL tests in this file are expected to FAIL (compile error or runtime failure)
// because the required Profile fields and applier passes are not yet implemented.
// The Green team must add the struct fields and pipeline passes described in
// /tmp/rgr/red/gum-np38.5.md to make these tests pass.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mustApplyJSON applies a profile to a JSON body and returns the decoded result
// as any (map or slice). Fails the test on any error.
func mustApplyJSON(t *testing.T, p *profile.Profile, bodyJSON string) any {
	t.Helper()
	out, err := profile.Apply(p, profile.ApplyInput{
		Body:       []byte(bodyJSON),
		UserFormat: "json",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var v any
	if err := json.Unmarshal(out.Body, &v); err != nil {
		t.Fatalf("unmarshal Apply output: %v\nbody: %s", err, string(out.Body))
	}
	return v
}

// mustApplyRaw applies a profile and returns raw output bytes. Format is caller-supplied.
func mustApplyRaw(t *testing.T, p *profile.Profile, bodyJSON string, format string) []byte {
	t.Helper()
	out, err := profile.Apply(p, profile.ApplyInput{
		Body:       []byte(bodyJSON),
		UserFormat: format,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return out.Body
}

// getMap asserts v is a map[string]any and returns it.
func getMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T: %v", v, v)
	}
	return m
}

// getSlice asserts v is a []any and returns it.
func getSlice(t *testing.T, v any) []any {
	t.Helper()
	s, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T: %v", v, v)
	}
	return s
}

// ---------------------------------------------------------------------------
// Test 1: keep_fields — recursive allowlist
// ---------------------------------------------------------------------------

// TestApplyKeepFieldsAllowlistRecursive verifies that keep_fields=["id","messages.id"]
// retains only allowed paths. The top-level "total" key must be dropped and
// the nested messages[].subj must be dropped, while messages[].id is retained.
// Spec: expression-profile-dsl.md Field Reference: keep_fields.
func TestApplyKeepFieldsAllowlistRecursive(t *testing.T) {
	p := &profile.Profile{
		// KeepFields is the new struct field — will not compile until Green adds it.
		KeepFields: []string{"id", "messages.id"},
	}

	body := `{"messages":[{"id":"a","subj":"x"},{"id":"b","subj":"y"}],"total":99}`
	v := mustApplyJSON(t, p, body)
	m := getMap(t, v)

	// top-level "total" must be absent — it is not in the allowlist.
	if _, hasTotal := m["total"]; hasTotal {
		t.Errorf("keep_fields: 'total' should have been dropped, still present: %v", m)
	}

	// messages array must be present.
	msgs, ok := m["messages"]
	if !ok {
		t.Fatalf("keep_fields: 'messages' should be retained (messages.id is in allowlist), got: %v", m)
	}
	arr := getSlice(t, msgs)
	if len(arr) != 2 {
		t.Fatalf("keep_fields: expected 2 messages, got %d", len(arr))
	}

	for i, elem := range arr {
		row := getMap(t, elem)
		if _, hasID := row["id"]; !hasID {
			t.Errorf("keep_fields: messages[%d].id must be retained", i)
		}
		if _, hasSubj := row["subj"]; hasSubj {
			t.Errorf("keep_fields: messages[%d].subj must be dropped (not in allowlist)", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 2: drop_fields — denylist applied after keep_fields
// ---------------------------------------------------------------------------

// TestApplyDropFieldsDenylistAfterKeep verifies that drop_fields runs AFTER
// keep_fields. Even though keep_fields includes "raw", drop_fields=["raw"]
// removes it. Final result should have only "id".
// Spec: expression-profile-dsl.md Processing Order step 2.
func TestApplyDropFieldsDenylistAfterKeep(t *testing.T) {
	p := &profile.Profile{
		KeepFields: []string{"id", "raw"},
		DropFields: []string{"raw"},
	}

	body := `{"id":"abc","raw":"secret","extra":"gone"}`
	v := mustApplyJSON(t, p, body)
	m := getMap(t, v)

	if _, hasID := m["id"]; !hasID {
		t.Errorf("drop_fields: 'id' must be retained")
	}
	if _, hasRaw := m["raw"]; hasRaw {
		t.Errorf("drop_fields: 'raw' was in keep_fields but drop_fields removes it; should be absent")
	}
	if _, hasExtra := m["extra"]; hasExtra {
		t.Errorf("drop_fields: 'extra' should have been dropped by keep_fields pass")
	}
}

// ---------------------------------------------------------------------------
// Test 3: strip_nulls — removes null, empty string, empty object, empty array
// ---------------------------------------------------------------------------

// TestApplyStripNullsRemovesNullsAndEmpties verifies that strip_nulls=true
// elides null values, empty strings, empty objects, and empty arrays.
// Only "e" (non-empty string) should survive.
// Spec: expression-profile-dsl.md Field Reference: strip_nulls.
// Note: unit tests treat all fields as null_elision_safe; variant-level
// safety validation is out of scope for this bead.
func TestApplyStripNullsRemovesNullsAndEmpties(t *testing.T) {
	p := &profile.Profile{
		StripNulls: true,
	}

	body := `{"a":null,"b":"","c":{},"d":[],"e":"x"}`
	v := mustApplyJSON(t, p, body)
	m := getMap(t, v)

	if _, hasA := m["a"]; hasA {
		t.Errorf("strip_nulls: 'a' (null) should be removed")
	}
	if _, hasB := m["b"]; hasB {
		t.Errorf("strip_nulls: 'b' (empty string) should be removed")
	}
	if _, hasC := m["c"]; hasC {
		t.Errorf("strip_nulls: 'c' (empty object) should be removed")
	}
	if _, hasD := m["d"]; hasD {
		t.Errorf("strip_nulls: 'd' (empty array) should be removed")
	}
	if val, hasE := m["e"]; !hasE || val != "x" {
		t.Errorf("strip_nulls: 'e' (non-empty string) must be retained with value \"x\", got: %v", m)
	}
	if len(m) != 1 {
		t.Errorf("strip_nulls: expected exactly 1 field ('e'), got %d fields: %v", len(m), m)
	}
}

// ---------------------------------------------------------------------------
// Test 4: flatten — unwraps items envelope
// ---------------------------------------------------------------------------

// TestApplyFlattenUnwrapsItemsEnvelope verifies that flatten=true on an
// {"items":[...]} object unwraps to just the inner array as top-level value.
// Spec: expression-profile-dsl.md Field Reference: flatten.
func TestApplyFlattenUnwrapsItemsEnvelope(t *testing.T) {
	p := &profile.Profile{
		Flatten: true,
	}

	body := `{"items":[1,2,3]}`
	v := mustApplyJSON(t, p, body)

	arr := getSlice(t, v)
	if len(arr) != 3 {
		t.Fatalf("flatten items: expected array of length 3, got %d: %v", len(arr), v)
	}
	if arr[0].(float64) != 1 || arr[1].(float64) != 2 || arr[2].(float64) != 3 {
		t.Errorf("flatten items: expected [1,2,3], got %v", arr)
	}
}

// ---------------------------------------------------------------------------
// Test 5: flatten — unwraps data envelope
// ---------------------------------------------------------------------------

// TestApplyFlattenUnwrapsDataEnvelope verifies that flatten=true also unwraps
// the common {"data":[...]} envelope shape.
// Spec: expression-profile-dsl.md Field Reference: flatten.
func TestApplyFlattenUnwrapsDataEnvelope(t *testing.T) {
	p := &profile.Profile{
		Flatten: true,
	}

	body := `{"data":[1,2]}`
	v := mustApplyJSON(t, p, body)

	arr := getSlice(t, v)
	if len(arr) != 2 {
		t.Fatalf("flatten data: expected array of length 2, got %d: %v", len(arr), v)
	}
	if arr[0].(float64) != 1 || arr[1].(float64) != 2 {
		t.Errorf("flatten data: expected [1,2], got %v", arr)
	}
}

// ---------------------------------------------------------------------------
// Test 6: collapse_arrays — truncates and records omitted_count
// ---------------------------------------------------------------------------

// TestApplyCollapseArraysTruncatesAndCountsOmissions verifies that
// collapse_arrays with max_items=2 on a 5-element array returns 2 items
// and records omitted_count=3. The brief specifies the wire shape:
// Apply wraps the result in a top-level object:
//
//	{"items": [<truncated array>], "omitted_count": <N>}
//
// when the top-level value IS an array after prior pipeline passes.
// Spec: expression-profile-dsl.md Sub-Fields: collapse_arrays.
func TestApplyCollapseArraysTruncatesAndCountsOmissions(t *testing.T) {
	p := &profile.Profile{
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 2},
	}

	body := `[1,2,3,4,5]`
	v := mustApplyJSON(t, p, body)
	m := getMap(t, v)

	// The wire shape is {"items":[...], "omitted_count": N}.
	itemsAny, hasItems := m["items"]
	if !hasItems {
		t.Fatalf("collapse_arrays: expected top-level 'items' key in result, got: %v", m)
	}
	items := getSlice(t, itemsAny)
	if len(items) != 2 {
		t.Errorf("collapse_arrays: expected 2 items, got %d: %v", len(items), items)
	}

	omittedAny, hasOmitted := m["omitted_count"]
	if !hasOmitted {
		t.Fatalf("collapse_arrays: expected top-level 'omitted_count' key in result, got: %v", m)
	}
	// JSON numbers unmarshal as float64.
	omitted, ok := omittedAny.(float64)
	if !ok {
		t.Fatalf("collapse_arrays: omitted_count is not a number: %T %v", omittedAny, omittedAny)
	}
	if int(omitted) != 3 {
		t.Errorf("collapse_arrays: omitted_count = %v; want 3", omitted)
	}
}

// ---------------------------------------------------------------------------
// Test 7: truncate_strings — default truncation
// ---------------------------------------------------------------------------

// TestApplyTruncateStringsDefault verifies that truncate_strings.default_chars=5
// truncates string field "a" from 8 to 5 chars and appends the truncation marker "…".
// Spec: expression-profile-dsl.md Sub-Fields: truncate_strings.
func TestApplyTruncateStringsDefault(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{
			DefaultChars: 5,
		},
	}

	body := `{"a":"abcdefgh"}`
	v := mustApplyJSON(t, p, body)
	m := getMap(t, v)

	val, ok := m["a"].(string)
	if !ok {
		t.Fatalf("truncate_strings: expected 'a' to be a string, got %T", m["a"])
	}
	// Must be truncated to 5 chars with truncation marker "…".
	if !strings.HasPrefix(val, "abcde") {
		t.Errorf("truncate_strings default: first 5 chars must be 'abcde', got %q", val)
	}
	if !strings.Contains(val, "…") {
		t.Errorf("truncate_strings default: truncation marker '…' must be present in %q", val)
	}
	// The original 8-char value must not appear in full.
	if val == "abcdefgh" {
		t.Errorf("truncate_strings default: value must be truncated, still has full original value %q", val)
	}
}

// ---------------------------------------------------------------------------
// Test 8: truncate_strings — per-field override
// ---------------------------------------------------------------------------

// TestApplyTruncateStringsPerFieldOverride verifies that fields listed in
// TruncateStringsSpec.Fields override the default. snippet gets 3 chars;
// other gets 8 chars (default).
// Spec: expression-profile-dsl.md Sub-Fields: truncate_strings.fields.
func TestApplyTruncateStringsPerFieldOverride(t *testing.T) {
	p := &profile.Profile{
		TruncateStrings: &profile.TruncateStringsSpec{
			DefaultChars: 8,
			Fields:       map[string]int{"snippet": 3},
		},
	}

	body := `{"snippet":"hello world","other":"hello world"}`
	v := mustApplyJSON(t, p, body)
	m := getMap(t, v)

	snippet, ok := m["snippet"].(string)
	if !ok {
		t.Fatalf("truncate_strings per-field: 'snippet' must be a string, got %T", m["snippet"])
	}
	other, ok := m["other"].(string)
	if !ok {
		t.Fatalf("truncate_strings per-field: 'other' must be a string, got %T", m["other"])
	}

	// snippet limited to 3 chars + marker.
	if len([]rune(strings.TrimRight(snippet, "…"))) > 3 {
		t.Errorf("truncate_strings per-field: snippet should be ≤3 chars (before marker), got %q (rune count %d)",
			snippet, len([]rune(snippet)))
	}
	if !strings.Contains(snippet, "…") {
		t.Errorf("truncate_strings per-field: snippet truncation marker missing: %q", snippet)
	}

	// other limited to 8 chars + marker: "hello wo…" (len 8 before marker).
	otherBase := strings.TrimRight(other, "…")
	if len([]rune(otherBase)) > 8 {
		t.Errorf("truncate_strings per-field: other should be ≤8 chars (before marker), got %q", other)
	}
	// "hello world" is 11 chars, so it MUST have been truncated.
	if other == "hello world" {
		t.Errorf("truncate_strings per-field: 'other' must be truncated (default_chars=8 < 11), still full: %q", other)
	}
}

// ---------------------------------------------------------------------------
// Test 9: dedupe — collapses duplicate rows, first-wins
// ---------------------------------------------------------------------------

// TestApplyDedupeByStableKey verifies that dedupe.by=["id"] keeps the first
// occurrence of rows with the same id. Input has two rows with id="a" and one
// with id="b"; output must have exactly 2 rows with first-occurrence semantics.
// Spec: expression-profile-dsl.md Sub-Fields: dedupe.
func TestApplyDedupeByStableKey(t *testing.T) {
	p := &profile.Profile{
		Dedupe: &profile.DedupeSpec{By: []string{"id"}},
	}

	body := `[{"id":"a","v":1},{"id":"a","v":2},{"id":"b","v":3}]`
	v := mustApplyJSON(t, p, body)
	arr := getSlice(t, v)

	if len(arr) != 2 {
		t.Fatalf("dedupe: expected 2 rows after dedup, got %d: %v", len(arr), arr)
	}

	row0 := getMap(t, arr[0])
	row1 := getMap(t, arr[1])

	// First occurrence of id="a" must have v=1.
	if row0["id"] != "a" {
		t.Errorf("dedupe: first row id must be 'a', got %v", row0["id"])
	}
	if row0["v"].(float64) != 1 {
		t.Errorf("dedupe: first row v must be 1 (first occurrence wins), got %v", row0["v"])
	}

	// id="b" must be present.
	if row1["id"] != "b" {
		t.Errorf("dedupe: second row id must be 'b', got %v", row1["id"])
	}
	if row1["v"].(float64) != 3 {
		t.Errorf("dedupe: second row v must be 3, got %v", row1["v"])
	}
}

// ---------------------------------------------------------------------------
// Test 10: on_empty — sentinel fired when pipeline produces zero records
// ---------------------------------------------------------------------------

// TestApplyOnEmptySentinelFires verifies that on_empty="No results." is
// surfaced when the pipeline produces an empty array. The response body
// must contain the sentinel string.
// Spec: expression-profile-dsl.md Field Reference: on_empty.
func TestApplyOnEmptySentinelFires(t *testing.T) {
	p := &profile.Profile{
		OnEmpty: "No results.",
	}

	// An empty array causes on_empty to fire (non-empty upstream → empty shaped result).
	body := `[]`
	out, err := profile.Apply(p, profile.ApplyInput{
		Body:       []byte(body),
		UserFormat: "json",
	})
	if err != nil {
		t.Fatalf("Apply on_empty: %v", err)
	}

	bodyStr := string(out.Body)
	if !strings.Contains(bodyStr, "No results.") {
		t.Errorf("on_empty: output body must contain sentinel %q, got: %s", "No results.", bodyStr)
	}
}

// ---------------------------------------------------------------------------
// Test 11: recovery — field round-trips through Profile struct
// ---------------------------------------------------------------------------

// TestApplyRecoveryFieldRoundtrip verifies that Profile.Recovery is an exposed
// string field and that Apply does not fail or panic when it is set.
// The side-effect (filesystem tee write) is out of scope for this bead.
// Spec: expression-profile-dsl.md Field Reference: recovery.
func TestApplyRecoveryFieldRoundtrip(t *testing.T) {
	p := &profile.Profile{
		Recovery: "local_artifact",
	}

	// Verify the field is set correctly.
	if p.Recovery != "local_artifact" {
		t.Errorf("Recovery field: got %q, want %q", p.Recovery, "local_artifact")
	}

	// Apply must not panic or error when Recovery is set.
	body := `{"id":"x"}`
	out, err := profile.Apply(p, profile.ApplyInput{
		Body:       []byte(body),
		UserFormat: "json",
	})
	if err != nil {
		t.Fatalf("Apply with Recovery='local_artifact': %v", err)
	}
	if len(out.Body) == 0 {
		t.Error("Apply with Recovery='local_artifact': output body is empty")
	}
}

// ---------------------------------------------------------------------------
// Test 12 (ordering): keep_fields runs before drop_fields
// ---------------------------------------------------------------------------

// TestApplyPipelineOrderKeepBeforeDrop explicitly tests the ordering guarantee:
// keep_fields narrows first, then drop_fields removes from the narrowed set.
// A field in neither list must not appear. A field in drop only (but not keep)
// must also not appear (keep already removed it).
// Spec: expression-profile-dsl.md Processing Order step 2.
func TestApplyPipelineOrderKeepBeforeDrop(t *testing.T) {
	p := &profile.Profile{
		KeepFields: []string{"a", "b"},
		DropFields: []string{"b"},
	}

	body := `{"a":1,"b":2,"c":3}`
	v := mustApplyJSON(t, p, body)
	m := getMap(t, v)

	// "a" kept by keep_fields, not in drop_fields — must be present.
	if _, hasA := m["a"]; !hasA {
		t.Errorf("pipeline order keep→drop: 'a' must be retained")
	}
	// "b" passed keep_fields but then dropped — must be absent.
	if _, hasB := m["b"]; hasB {
		t.Errorf("pipeline order keep→drop: 'b' must be dropped (in drop_fields)")
	}
	// "c" never passed keep_fields — must be absent.
	if _, hasC := m["c"]; hasC {
		t.Errorf("pipeline order keep→drop: 'c' must not appear (not in keep_fields)")
	}
	if len(m) != 1 {
		t.Errorf("pipeline order keep→drop: expected exactly 1 field ('a'), got %d: %v", len(m), m)
	}
}

// ---------------------------------------------------------------------------
// Test 13 (ordering): collapse_arrays runs before format encoding
// ---------------------------------------------------------------------------

// TestApplyPipelineOrderCollapseBeforeFormat verifies that collapse_arrays
// truncates the array before the format pass. When format=toon, the TOON
// output should reflect exactly 2 items (not 5).
// Spec: expression-profile-dsl.md Processing Order steps 5, 8.
func TestApplyPipelineOrderCollapseBeforeFormat(t *testing.T) {
	p := &profile.Profile{
		CollapseArrays: &profile.CollapseArraysSpec{MaxItems: 2},
	}

	// Use toon output to verify count.
	body := `[{"id":"a"},{"id":"b"},{"id":"c"},{"id":"d"},{"id":"e"}]`
	outBytes := mustApplyRaw(t, p, body, "toon")
	outStr := string(outBytes)

	// The TOON output must contain exactly 2 "id" rows, not 5.
	// Count occurrences of "id" in the toon output.
	// A simple count of id= occurrences distinguishes 2 vs 5 items.
	idCount := strings.Count(outStr, "id")
	if idCount > 3 {
		// 2 items → 2 "id" occurrences in toon (one per row), possibly + 1 in omitted meta.
		// 5 items → 5+ "id" occurrences.
		t.Errorf("pipeline order collapse→format: TOON output appears to contain more than 2 items (found %d 'id' occurrences); collapse_arrays must run before format encoding.\nOutput:\n%s", idCount, outStr)
	}
	if idCount == 0 {
		t.Errorf("pipeline order collapse→format: no 'id' in TOON output at all; output:\n%s", outStr)
	}
}
