package dispatch

import (
	"strings"
	"testing"
)

// TestCheckArgType exercises all four declared-type branches plus the
// no-match fall-through. The contract: return "" when val matches
// declType, otherwise a "<name>: expected X, got <Go type>" message so
// upstream validation can surface the offending parameter cleanly.
func TestCheckArgType(t *testing.T) {
	cases := []struct {
		name       string
		declType   string
		val        any
		wantOK     bool
		wantSubstr string
	}{
		// string
		{"string_ok", "string", "x", true, ""},
		{"string_wrong", "string", 1, false, "expected string"},

		// integer accepts every numeric type
		{"int_ok", "integer", 1, true, ""},
		{"int64_ok", "integer", int64(2), true, ""},
		{"uint_ok", "integer", uint(3), true, ""},
		{"float32_ok", "integer", float32(4), true, ""},
		{"float64_ok", "integer", float64(5), true, ""},
		// A string that PARSES as an integer is accepted (query/path params are
		// strings on the wire; `key=value` yields a string) — the F4 fix.
		{"int_str_ok", "integer", "2", true, ""},
		{"int_wrong_type", "integer", "abc", false, "expected integer"},

		// bool — a string "true"/"false" is accepted (query bool param), but a
		// non-bool string is still rejected.
		{"bool_ok", "bool", true, true, ""},
		{"bool_str_ok", "bool", "true", true, ""},
		{"bool_str_wrong", "bool", "maybe", false, "expected bool"},

		// string[]
		{"string_slice_ok", "string[]", []string{"a", "b"}, true, ""},
		{"any_slice_all_strings_ok", "string[]", []any{"a", "b"}, true, ""},
		{"any_slice_mixed_fails", "string[]", []any{"a", 1}, false, "string[] but element is int"},
		{"string_slice_wrong_outer", "string[]", "not-a-slice", false, "expected string[]"},

		// unknown declType → no validation, returns "" (the catch-all branch).
		{"unknown_type_passes", "uuid", "anything", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkArgType("p", tc.val, tc.declType)
			if tc.wantOK {
				if got != "" {
					t.Errorf("got %q; want \"\"", got)
				}
				return
			}
			if got == "" {
				t.Fatalf("got \"\"; want error message")
			}
			if !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("got %q; want substring %q", got, tc.wantSubstr)
			}
			if !strings.HasPrefix(got, "p:") {
				t.Errorf("got %q; want 'p:' prefix (param name)", got)
			}
		})
	}
}
