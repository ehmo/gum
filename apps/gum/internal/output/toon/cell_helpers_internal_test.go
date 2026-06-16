package toon

import "testing"

// TestEncodeDocumentCellShapes pins the §9.0 CSV-cell projection across
// the full type matrix. Untyped numerics need separate cases from
// float64 because dispatch can hand either side back depending on
// whether the value flowed through json.Unmarshal first. The default
// branch routes anything unrecognized through encodeDocumentString so
// quoting rules still apply.
func TestEncodeDocumentCellShapes(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil_empty", nil, ""},
		{"empty_string_quoted", "", `""`},
		{"bare_string", "hello", "hello"},
		{"string_needs_quote", "a,b", `"a,b"`},
		{"bool_true", true, "true"},
		{"bool_false", false, "false"},
		{"float64_whole", float64(42), "42"},
		{"float64_fraction", float64(1.5), "1.5"},
		{"int", 7, "7"},
		{"int64", int64(-3), "-3"},
		{"int32", int32(99), "99"},
		{"default_uses_string_encoder", uint32(5), "5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeDocumentCell(tc.in)
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if got != tc.want {
				t.Errorf("got=%q; want %q", got, tc.want)
			}
		})
	}
}

// TestTypedCellToValueShapes pins the typed-pointer dereference path
// used by EncodeTOONDocument. nil pointers must collapse to untyped
// nil so the CSV cell emits empty (not the literal string "<nil>"),
// and the default branch must round-trip non-pointer values verbatim
// so the encoder can still classify them.
func TestTypedCellToValueShapes(t *testing.T) {
	s := "abc"
	i := int64(42)
	f := 1.5
	b := true

	cases := []struct {
		name string
		in   any
		want any
	}{
		{"untyped_nil", nil, nil},
		{"string_ptr", &s, "abc"},
		{"string_nil_ptr", (*string)(nil), nil},
		{"int64_ptr", &i, int64(42)},
		{"int64_nil_ptr", (*int64)(nil), nil},
		{"float64_ptr", &f, 1.5},
		{"float64_nil_ptr", (*float64)(nil), nil},
		{"bool_ptr", &b, true},
		{"bool_nil_ptr", (*bool)(nil), nil},
		{"default_value", 7, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := typedCellToValue(tc.in)
			if got != tc.want {
				t.Errorf("got=%v; want %v", got, tc.want)
			}
		})
	}
}
