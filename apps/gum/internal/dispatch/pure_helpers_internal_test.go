package dispatch

import (
	"math"
	"strings"
	"testing"
)

// TestCheckArgTypeBoolArms covers the "bool" declType. A `key=value` positional
// yields a string, so "true" must pass while a word ParseBool rejects must not.
func TestCheckArgTypeBoolArms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"native true", true, false},
		{"native false", false, false},
		{"string true", "true", false},
		{"string 1", "1", false},
		{"string F", "F", false},
		{"non-boolean word", "yes", true},
		{"number", 1, true},
		{"nil", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkArgType("flag", tc.val, "bool")
			if tc.wantErr && got == "" {
				t.Errorf("checkArgType(%v as bool) = \"\"; want a type error", tc.val)
			}
			if !tc.wantErr && got != "" {
				t.Errorf("checkArgType(%v as bool) = %q; want no error", tc.val, got)
			}
		})
	}
}

// TestCheckArgTypeStringSliceArms covers "string[]". JSON arrays decode to
// []any, so the element walk is the shape every MCP arg arrives in.
func TestCheckArgTypeStringSliceArms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		val     any
		wantErr bool
	}{
		{"native string slice", []string{"a", "b"}, false},
		{"empty native slice", []string{}, false},
		{"decoded json array", []any{"a", "b"}, false},
		{"empty decoded array", []any{}, false},
		{"decoded array with a number", []any{"a", float64(2)}, true},
		{"decoded array with nil", []any{nil}, true},
		{"bare string", "a,b", true},
		{"int slice", []int{1}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkArgType("fields", tc.val, "string[]")
			if tc.wantErr && got == "" {
				t.Errorf("checkArgType(%v as string[]) = \"\"; want a type error", tc.val)
			}
			if !tc.wantErr && got != "" {
				t.Errorf("checkArgType(%v as string[]) = %q; want no error", tc.val, got)
			}
		})
	}
}

// TestCanonicalizeArgsEmptyShapes pins the two "{}" returns. A map that holds
// only nulls prunes to nothing per spec §10.0 Rule 1, so it must hash the same
// as a caller who omitted the keys entirely.
func TestCanonicalizeArgsEmptyShapes(t *testing.T) {
	t.Parallel()
	cases := map[string]map[string]any{
		"nil map":       nil,
		"empty map":     {},
		"all nulls":     {"a": nil, "b": nil},
		"nested absent": {"only": nil},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := canonicalizeArgs(args); got != "{}" {
				t.Errorf("canonicalizeArgs(%v) = %q; want \"{}\"", args, got)
			}
		})
	}
}

// TestCanonicalizeArgsFallsBackWhenJCSRefuses covers the programming-error
// path: jcs.Marshal rejects NaN, and collapsing to "{}" there would make two
// distinct arg sets share a cache key. The fallback must stay distinguishing.
func TestCanonicalizeArgsFallsBackWhenJCSRefuses(t *testing.T) {
	t.Parallel()
	got := canonicalizeArgs(map[string]any{"a": 1, "bad": math.NaN()})
	if got == "{}" {
		t.Fatal("canonicalizeArgs collapsed a NaN arg set to \"{}\"")
	}
	if !strings.Contains(got, `"a":1`) {
		t.Errorf("canonicalizeArgs = %q; want the usable key preserved", got)
	}
	other := canonicalizeArgs(map[string]any{"a": 2, "bad": math.NaN()})
	if other == got {
		t.Errorf("two distinct arg sets both canonicalized to %q", got)
	}
}

// TestFallbackCanonicalArgsSortsKeys pins the fallback's own contract: keys in
// byte order, no trailing comma, so the same map always yields the same bytes.
func TestFallbackCanonicalArgsSortsKeys(t *testing.T) {
	t.Parallel()
	args := map[string]any{"b": 2, "a": 1, "c": "x"}
	want := `{"a":1,"b":2,"c":"x"}`
	if got := fallbackCanonicalArgs(args); got != want {
		t.Errorf("fallbackCanonicalArgs = %q; want %q", got, want)
	}
	if got := fallbackCanonicalArgs(map[string]any{"only": true}); got != `{"only":true}` {
		t.Errorf("single-key fallback = %q; want no trailing comma", got)
	}
}

// TestDestructiveScopeCanonicalShapes covers the three returns. An absent or
// null destructive_scope must hash identically to an explicit empty list, or a
// confirmation token issued for one spelling fails to match the other.
func TestDestructiveScopeCanonicalShapes(t *testing.T) {
	t.Parallel()
	if got := destructiveScopeCanonical(nil); got != "[]" {
		t.Errorf("destructiveScopeCanonical(nil) = %q; want \"[]\"", got)
	}
	if got := destructiveScopeCanonical(map[string]any{"other": 1}); got != "[]" {
		t.Errorf("destructiveScopeCanonical(no key) = %q; want \"[]\"", got)
	}
	if got := destructiveScopeCanonical(map[string]any{"destructive_scope": nil}); got != "[]" {
		t.Errorf("destructiveScopeCanonical(null) = %q; want \"[]\"", got)
	}
	want := `{"destructive_scope":["a","b"]}`
	got := destructiveScopeCanonical(map[string]any{"destructive_scope": []any{"a", "b"}})
	if got != want {
		t.Errorf("destructiveScopeCanonical(list) = %q; want %q", got, want)
	}
}

// TestLevenshteinEmptyOperands covers the two early returns. An empty operand
// costs one edit per rune of the other side.
func TestLevenshteinEmptyOperands(t *testing.T) {
	t.Parallel()
	if got := levenshtein("", "gmail"); got != 5 {
		t.Errorf("levenshtein(\"\", \"gmail\") = %d; want 5", got)
	}
	if got := levenshtein("gmail", ""); got != 5 {
		t.Errorf("levenshtein(\"gmail\", \"\") = %d; want 5", got)
	}
	if got := levenshtein("", ""); got != 0 {
		t.Errorf("levenshtein(\"\", \"\") = %d; want 0", got)
	}
}

// TestSuggestOpIDsShortQueryKeepsFloorDistance covers the maxDist floor. A
// two-character query has len(q)/2 == 1, so without the floor a one-edit typo
// would be the only thing ever suggested.
func TestSuggestOpIDsShortQueryKeepsFloorDistance(t *testing.T) {
	t.Parallel()
	got := suggestOpIDs("ab", []string{"abcd", "zzzzzz"}, 3)
	if len(got) != 1 || got[0] != "abcd" {
		t.Errorf("suggestOpIDs(\"ab\", ...) = %v; want [abcd]", got)
	}
}

// TestSuggestOpIDsStopsAtTheLimit covers the break. More candidates sit inside
// the distance threshold than the caller asked for, and the cap must hold.
func TestSuggestOpIDsStopsAtTheLimit(t *testing.T) {
	t.Parallel()
	candidates := []string{"gmail.lista", "gmail.listb", "gmail.listc", "gmail.listd"}
	got := suggestOpIDs("gmail.list", candidates, 2)
	if len(got) != 2 {
		t.Fatalf("suggestOpIDs limit 2 returned %d entries: %v", len(got), got)
	}
	if got[0] != "gmail.lista" || got[1] != "gmail.listb" {
		t.Errorf("suggestOpIDs = %v; want the two lowest-distance ids", got)
	}
}
