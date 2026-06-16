package toon

import (
	"reflect"
	"testing"
)

// TestParseCSVRowShapes exercises the §9.0 CSV row parser across the
// full state machine: bare fields, quoted fields, doubled-quote
// escapes inside quotes, commas inside quotes, and the always-final
// trailing-field append (no terminating comma case). Drift here
// produces silently mis-split rows downstream.
func TestParseCSVRowShapes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single_bare", "abc", []string{"abc"}},
		{"two_bare", "a,b", []string{"a", "b"}},
		{"empty_fields", "a,,c", []string{"a", "", "c"}},
		{"quoted_simple", `"a","b"`, []string{"a", "b"}},
		{"quoted_with_comma", `"a,b",c`, []string{"a,b", "c"}},
		{"doubled_quote_escape", `"he said ""hi"""`, []string{`he said "hi"`}},
		{"empty", "", []string{""}},
		{"trailing_comma", "a,", []string{"a", ""}},
		{"quoted_then_bare", `"x",y,z`, []string{"x", "y", "z"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCSVRow(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got=%#v; want %#v", got, tc.want)
			}
		})
	}
}
