// Package fieldmask_test contains tests for the fieldmask package.
// These tests are written red-team style: no implementation exists yet.
// The green team builds the implementation to satisfy these contracts.
//
// Acceptance criteria (gum-np38.4):
//   - items(id,name,siblings(*)) parses to typed Go tree
//   - resolver projects map[string]any correctly
//   - used by DSL evaluator at stage 1
//
// Import path: github.com/ehmo/gum/internal/output/fieldmask
package fieldmask_test

import (
	"testing"

	"github.com/ehmo/gum/internal/output/fieldmask"
)

// ---------------------------------------------------------------------------
// Parse tests
// ---------------------------------------------------------------------------

// TestFieldMaskParseSimple verifies that a flat comma-separated list parses
// and that Has returns correct results for present and absent fields.
func TestFieldMaskParseSimple(t *testing.T) {
	m, err := fieldmask.Parse("id,name")
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", "id,name", err)
	}
	if !m.Has("id") {
		t.Error("Has(\"id\") should be true")
	}
	if !m.Has("name") {
		t.Error("Has(\"name\") should be true")
	}
	if m.Has("other") {
		t.Error("Has(\"other\") should be false")
	}
}

// TestFieldMaskParseNested verifies that a nested selector like "user(id,email)"
// produces a mask where Has("user","id") and Has("user","email") are true but
// Has("user","phone") is false.
func TestFieldMaskParseNested(t *testing.T) {
	m, err := fieldmask.Parse("user(id,email)")
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", "user(id,email)", err)
	}
	if !m.Has("user", "id") {
		t.Error("Has(\"user\",\"id\") should be true")
	}
	if !m.Has("user", "email") {
		t.Error("Has(\"user\",\"email\") should be true")
	}
	if m.Has("user", "phone") {
		t.Error("Has(\"user\",\"phone\") should be false")
	}
	// The top-level field "user" itself is implicitly present.
	if !m.Has("user") {
		t.Error("Has(\"user\") should be true (implicit container)")
	}
	// A sibling field at top-level that was not declared should be absent.
	if m.Has("id") {
		t.Error("Has(\"id\") at top level should be false when only user(id) declared")
	}
}

// TestFieldMaskParseDeepNested verifies three-level nesting:
// "items(id,siblings(name,age))".
func TestFieldMaskParseDeepNested(t *testing.T) {
	m, err := fieldmask.Parse("items(id,siblings(name,age))")
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", "items(id,siblings(name,age))", err)
	}
	if !m.Has("items", "id") {
		t.Error("Has(\"items\",\"id\") should be true")
	}
	if !m.Has("items", "siblings", "name") {
		t.Error("Has(\"items\",\"siblings\",\"name\") should be true")
	}
	if !m.Has("items", "siblings", "age") {
		t.Error("Has(\"items\",\"siblings\",\"age\") should be true")
	}
	if m.Has("items", "siblings", "email") {
		t.Error("Has(\"items\",\"siblings\",\"email\") should be false")
	}
}

// TestFieldMaskParseWildcard verifies that "items(*)" means all fields under
// items are selected: Has("items","anything") must be true.
func TestFieldMaskParseWildcard(t *testing.T) {
	m, err := fieldmask.Parse("items(*)")
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", "items(*)", err)
	}
	if !m.Has("items", "anything") {
		t.Error("Has(\"items\",\"anything\") should be true under wildcard")
	}
	if !m.Has("items", "id") {
		t.Error("Has(\"items\",\"id\") should be true under wildcard")
	}
	if !m.Has("items", "name") {
		t.Error("Has(\"items\",\"name\") should be true under wildcard")
	}
}

// TestFieldMaskParseMixedTopLevel verifies that top-level scalars and a nested
// object coexist: "id,name,user(email)".
func TestFieldMaskParseMixedTopLevel(t *testing.T) {
	m, err := fieldmask.Parse("id,name,user(email)")
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", "id,name,user(email)", err)
	}
	if !m.Has("id") {
		t.Error("Has(\"id\") should be true")
	}
	if !m.Has("name") {
		t.Error("Has(\"name\") should be true")
	}
	if !m.Has("user", "email") {
		t.Error("Has(\"user\",\"email\") should be true")
	}
	if m.Has("user", "phone") {
		t.Error("Has(\"user\",\"phone\") should be false")
	}
	if m.Has("extra") {
		t.Error("Has(\"extra\") should be false")
	}
}

// TestFieldMaskParseErrors is a table-driven test for malformed inputs that
// must produce non-nil errors. No mask should be returned on error.
func TestFieldMaskParseErrors(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"unmatched_open_paren", "items("},
		{"empty_parens", "items()"},
		{"leading_comma", ",id"},
		{"trailing_comma", "id,"},
		{"empty_string", ""},
		{"double_comma", "id,,name"},
		// parseIdent's non-EOF error branch: a leading char that fails
		// isIdentStart but is also not one of the punctuation chars
		// parseMask pre-checks for.
		{"digit_start", "1foo"},
		{"symbol_start", "@bad"},
		// parseIdent's EOF branch via a nested mask that ends right at the
		// inner identifier slot: items( has no body and no closing paren,
		// so parseField → parseIdent sees byte 0 mid-parse.
		{"nested_eof_ident", "id,items("},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := fieldmask.Parse(tc.input)
			if err == nil {
				t.Errorf("Parse(%q) expected error, got nil (mask=%v)", tc.input, m)
			}
			if m != nil {
				t.Errorf("Parse(%q) returned non-nil mask on error", tc.input)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Project tests
// ---------------------------------------------------------------------------

// TestFieldMaskProjectFlat verifies that flat projection drops undeclared fields.
// Input: {"id":1, "name":"x", "extra":"y"}, mask "id,name"
// Expected: {"id":1, "name":"x"}
func TestFieldMaskProjectFlat(t *testing.T) {
	m, err := fieldmask.Parse("id,name")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	src := map[string]any{
		"id":    1,
		"name":  "x",
		"extra": "y",
	}
	got := m.Project(src)
	if len(got) != 2 {
		t.Errorf("expected 2 fields, got %d: %v", len(got), got)
	}
	if got["id"] != 1 {
		t.Errorf("id: want 1, got %v", got["id"])
	}
	if got["name"] != "x" {
		t.Errorf("name: want \"x\", got %v", got["name"])
	}
	if _, present := got["extra"]; present {
		t.Error("extra should be absent from result")
	}
}

// TestFieldMaskProjectNested verifies nested projection.
// Input: {"user":{"id":1, "email":"a", "phone":"b"}}, mask "user(id,email)"
// Expected: {"user":{"id":1, "email":"a"}}
func TestFieldMaskProjectNested(t *testing.T) {
	m, err := fieldmask.Parse("user(id,email)")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	src := map[string]any{
		"user": map[string]any{
			"id":    1,
			"email": "a",
			"phone": "b",
		},
	}
	got := m.Project(src)
	user, ok := got["user"].(map[string]any)
	if !ok {
		t.Fatalf("expected got[\"user\"] to be map[string]any, got %T", got["user"])
	}
	if user["id"] != 1 {
		t.Errorf("user.id: want 1, got %v", user["id"])
	}
	if user["email"] != "a" {
		t.Errorf("user.email: want \"a\", got %v", user["email"])
	}
	if _, present := user["phone"]; present {
		t.Error("user.phone should be absent from result")
	}
}

// TestFieldMaskProjectArray verifies that projection descends into array elements.
// Input: {"items":[{"id":1,"x":"a"},{"id":2,"x":"b"}]}, mask "items(id)"
// Expected: {"items":[{"id":1},{"id":2}]}
func TestFieldMaskProjectArray(t *testing.T) {
	m, err := fieldmask.Parse("items(id)")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	src := map[string]any{
		"items": []any{
			map[string]any{"id": 1, "x": "a"},
			map[string]any{"id": 2, "x": "b"},
		},
	}
	got := m.Project(src)
	rawItems, ok := got["items"]
	if !ok {
		t.Fatal("expected \"items\" key in result")
	}
	items, ok := rawItems.([]any)
	if !ok {
		t.Fatalf("expected items to be []any, got %T", rawItems)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	for i, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			t.Fatalf("items[%d] expected map[string]any, got %T", i, rawItem)
		}
		if _, present := item["x"]; present {
			t.Errorf("items[%d].x should be absent", i)
		}
		if item["id"] == nil {
			t.Errorf("items[%d].id should be present", i)
		}
	}
}

// TestFieldMaskProjectWildcardKeepsAll verifies that "items(*)" returns items
// completely unchanged (all fields preserved).
func TestFieldMaskProjectWildcardKeepsAll(t *testing.T) {
	m, err := fieldmask.Parse("items(*)")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	src := map[string]any{
		"items": []any{
			map[string]any{"id": 1, "name": "alpha", "extra": true},
		},
	}
	got := m.Project(src)
	rawItems, ok := got["items"]
	if !ok {
		t.Fatal("expected \"items\" key in result")
	}
	items, ok := rawItems.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", rawItems)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item expected map[string]any, got %T", items[0])
	}
	if _, present := item["id"]; !present {
		t.Error("item.id should be present under wildcard")
	}
	if _, present := item["name"]; !present {
		t.Error("item.name should be present under wildcard")
	}
	if _, present := item["extra"]; !present {
		t.Error("item.extra should be present under wildcard")
	}
}

// TestFieldMaskProjectMissingFields verifies that when a mask requests a field
// absent from the source, the result simply omits that field (no nil, no panic,
// no error).
func TestFieldMaskProjectMissingFields(t *testing.T) {
	m, err := fieldmask.Parse("id,missing_field")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	src := map[string]any{
		"id": 42,
	}
	got := m.Project(src)
	if got["id"] != 42 {
		t.Errorf("id: want 42, got %v", got["id"])
	}
	// missing_field should not appear in result (not nil, just absent).
	if v, present := got["missing_field"]; present {
		t.Errorf("missing_field should be absent, got %v", v)
	}
}

// ---------------------------------------------------------------------------
// Round-trip stringer test
// ---------------------------------------------------------------------------

// TestFieldMaskRoundTripStringer verifies that m.String() returns a canonical
// form that re-parses to an equivalent mask. Equivalence is checked via Has on
// a representative set of paths.
func TestFieldMaskRoundTripStringer(t *testing.T) {
	inputs := []struct {
		expr  string
		paths [][]string // paths that must be true after round-trip
	}{
		{
			expr:  "id,name",
			paths: [][]string{{"id"}, {"name"}},
		},
		{
			expr:  "items(id,siblings(name,age))",
			paths: [][]string{{"items", "id"}, {"items", "siblings", "name"}, {"items", "siblings", "age"}},
		},
		{
			expr:  "items(*)",
			paths: [][]string{{"items", "anythingAtAll"}},
		},
		{
			expr:  "id,name,user(email)",
			paths: [][]string{{"id"}, {"name"}, {"user", "email"}},
		},
	}
	for _, tc := range inputs {
		t.Run(tc.expr, func(t *testing.T) {
			m1, err := fieldmask.Parse(tc.expr)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.expr, err)
			}
			canonical := m1.String()
			if canonical == "" {
				t.Fatal("String() returned empty string")
			}
			m2, err := fieldmask.Parse(canonical)
			if err != nil {
				t.Fatalf("Parse(canonical %q): %v", canonical, err)
			}
			for _, path := range tc.paths {
				if !m2.Has(path...) {
					t.Errorf("after round-trip: Has(%v) should be true", path)
				}
			}
		})
	}
}

// TestMaskHasEmptyPathFalse pins the zero-segment guard: Has() with no
// arguments must return false, otherwise the recursive walker would
// dereference path[0] and panic. Used as the early-out in callers that
// build the path slice from a tokenizer that may emit empty tails.
func TestMaskHasEmptyPathFalse(t *testing.T) {
	m, err := fieldmask.Parse("id,name")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.Has() {
		t.Errorf("Has() with no args returned true; want false")
	}
}
