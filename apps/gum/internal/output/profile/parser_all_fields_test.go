package profile

import (
	"reflect"
	"testing"
)

// TestParseAllDocumentedFields pins gum-0hv9: the DSL parser handles the full
// documented field set (scalars, arrays, and inline-table objects), not just the
// original 7. Before the fix, strip_nulls/keep_fields/collapse_arrays/... failed
// with "unknown key".
func TestParseAllDocumentedFields(t *testing.T) {
	src := `default_format = "toon"
inherits = "_base.list_ops"
projection = ["id", "subject"]
keep_fields = ["messages.id", "messages.subject"]
drop_fields = ["raw"]
strip_nulls = true
flatten = true
flatten_singletons = true
omit_zero_counts = true
sort_by = "date"
limit = 50
field_mask_mode = "upstream"
collapse_arrays = { max_items = 20 }
truncate_strings = { default_chars = 500, fields = { snippet = 180, body = 90 } }
dedupe = { by = ["id"] }
on_empty = "No matching messages."
recovery = "local_artifact"
tee_mode = "always"
`
	p, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Inherits != "_base.list_ops" {
		t.Errorf("inherits=%q", p.Inherits)
	}
	if len(p.KeepFields) != 2 || p.KeepFields[0] != "messages.id" {
		t.Errorf("keep_fields=%v", p.KeepFields)
	}
	if len(p.DropFields) != 1 || p.DropFields[0] != "raw" {
		t.Errorf("drop_fields=%v", p.DropFields)
	}
	if !p.StripNulls || !p.Flatten {
		t.Errorf("strip_nulls=%v flatten=%v", p.StripNulls, p.Flatten)
	}
	if p.CollapseArrays == nil || p.CollapseArrays.MaxItems != 20 {
		t.Errorf("collapse_arrays=%+v", p.CollapseArrays)
	}
	if p.TruncateStrings == nil || p.TruncateStrings.DefaultChars != 500 ||
		p.TruncateStrings.Fields["snippet"] != 180 || p.TruncateStrings.Fields["body"] != 90 {
		t.Errorf("truncate_strings=%+v", p.TruncateStrings)
	}
	if p.Dedupe == nil || len(p.Dedupe.By) != 1 || p.Dedupe.By[0] != "id" {
		t.Errorf("dedupe=%+v", p.Dedupe)
	}
	if p.OnEmpty != "No matching messages." || p.Recovery != "local_artifact" || p.TeeMode != "always" {
		t.Errorf("on_empty=%q recovery=%q tee_mode=%q", p.OnEmpty, p.Recovery, p.TeeMode)
	}

	// Round-trip: Serialize -> Parse must reproduce the same Profile.
	p2, err := Parse(p.Serialize())
	if err != nil {
		t.Fatalf("re-Parse(Serialize):\n%s\nerr=%v", p.Serialize(), err)
	}
	if !reflect.DeepEqual(p, p2) {
		t.Errorf("round-trip mismatch:\n  serialized:\n%s\n  p1=%+v\n  p2=%+v", p.Serialize(), p, p2)
	}
}

// TestParseRejectsInvalidEnumsAndSubKeys pins the new validation: bad recovery /
// tee_mode values and unknown inline-table sub-keys are rejected, not silently
// accepted.
func TestParseRejectsInvalidEnumsAndSubKeys(t *testing.T) {
	bad := []string{
		`recovery = "bogus"`,
		`tee_mode = "sometimes"`,
		`collapse_arrays = { max_items = -1 }`,
		`collapse_arrays = { bogus = 1 }`,
		`truncate_strings = { fields = { x = 0 } }`,
		`dedupe = { bogus = ["a"] }`,
	}
	for _, src := range bad {
		if _, err := Parse(src); err == nil {
			t.Errorf("Parse(%q) = nil err; want rejection", src)
		}
	}
}
