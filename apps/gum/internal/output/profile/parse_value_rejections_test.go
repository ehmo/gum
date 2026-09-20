package profile_test

import (
	"strings"
	"testing"

	"github.com/ehmo/gum/internal/output/profile"
)

// TestParseRejectsMalformedScalarValues walks every scalar key in the DSL with
// a value of the wrong shape. Each key owns its own error arm in
// parseProfileKey, so one untested key means a profile with that typo parses
// into a zero value and the runtime shapes output with a setting the author
// never wrote.
func TestParseRejectsMalformedScalarValues(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"field_mask = nope", "field_mask"},
		{"inherits = nope", "inherits"},
		{"keep_fields = nope", "keep_fields"},
		{"drop_fields = nope", "drop_fields"},
		{"strip_nulls = yes", "strip_nulls"},
		{"flatten = yes", "flatten"},
		{"on_empty = nope", "on_empty"},
		{"recovery = nope", "recovery"},
		{"tee_mode = nope", "tee_mode"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			_, err := profile.Parse(tc.src)
			if err == nil {
				t.Fatalf("Parse(%q) err=nil; want a rejection", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err=%v; want the key %q named", err, tc.want)
			}
		})
	}
}

// TestParseRejectsMalformedInlineTables covers the sub-key arms of
// collapse_arrays, truncate_strings and dedupe. These three keys are the only
// nested values in the DSL, so their parsers carry the whole inline-table
// grammar: a missing '=', a duplicate key, a non-integer count, a negative
// count and an unknown sub-key.
func TestParseRejectsMalformedInlineTables(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"not_a_table", "truncate_strings = notatable", "expected inline table"},
		{"dedupe_not_a_table", "dedupe = notatable", "expected inline table"},
		{"entry_missing_equals", "collapse_arrays = { max_items }", "missing '='"},
		{"duplicate_sub_key", "collapse_arrays = { max_items = 1, max_items = 2 }", "duplicate key"},
		{"max_items_not_int", "collapse_arrays = { max_items = x }", "max_items"},
		{"max_items_negative", "collapse_arrays = { max_items = -1 }", "must be >= 0"},
		{"collapse_unknown_sub_key", "collapse_arrays = { bogus = 1 }", "unknown sub-key"},
		{"default_chars_not_int", "truncate_strings = { default_chars = x }", "default_chars"},
		{"default_chars_negative", "truncate_strings = { default_chars = -1 }", "must be >= 0"},
		{"fields_not_a_table", "truncate_strings = { fields = nope }", "fields"},
		{"fields_value_not_int", "truncate_strings = { fields = { title = x } }", "fields.title"},
		{"fields_value_below_one", "truncate_strings = { fields = { title = 0 } }", "must be >= 1"},
		{"truncate_unknown_sub_key", "truncate_strings = { bogus = 1 }", "unknown sub-key"},
		{"dedupe_by_not_array", "dedupe = { by = nope }", "by"},
		{"dedupe_unknown_sub_key", "dedupe = { bogus = 1 }", "unknown sub-key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := profile.Parse(tc.src)
			if err == nil {
				t.Fatalf("Parse(%q) err=nil; want a rejection", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err=%v; want %q in the message", err, tc.want)
			}
		})
	}
}

// TestParseAcceptsInlineTableWhitespaceForms pins the two grammar shapes that
// must not be errors: an empty table and a trailing comma. Both reach
// parseInlineTable arms that a rejection test never runs.
func TestParseAcceptsInlineTableWhitespaceForms(t *testing.T) {
	cases := map[string]string{
		"empty_table":    "collapse_arrays = { }\non_empty = \"no rows\"\n",
		"trailing_comma": "collapse_arrays = { max_items = 5, }\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := profile.Parse(src)
			if err != nil {
				t.Fatalf("Parse(%q): %v", src, err)
			}
			if p.CollapseArrays == nil {
				t.Fatal("collapse_arrays parsed to nil; want a spec")
			}
		})
	}
}

// TestParseRejectsZeroMaxItemsWithoutOnEmpty pins the one cross-field rule
// Parse can only check after the last line: a cap of 0 drops every row, and
// spec §13 forbids reporting that with no message saying why.
func TestParseRejectsZeroMaxItemsWithoutOnEmpty(t *testing.T) {
	_, err := profile.Parse("collapse_arrays = { max_items = 0 }\n")
	if err == nil {
		t.Fatal("Parse(max_items=0, no on_empty) err=nil; want a rejection")
	}
	if !strings.Contains(err.Error(), "on_empty") {
		t.Errorf("err=%v; want on_empty named as the missing piece", err)
	}
}
