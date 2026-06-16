package config

import "testing"

// TestUnquoteValueShapes pins the TOML scalar-unquoting matrix used
// when loading config.toml. The "" and ” cases preserve the
// distinction between quoted and bare scalars so values like `true`
// vs `"true"` parse differently downstream.
func TestUnquoteValueShapes(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty_rejected", "", "", true},
		{"double_quoted", `"abc"`, "abc", false},
		{"single_quoted", `'abc'`, "abc", false},
		{"bare_word", "true", "true", false},
		{"bare_number", "42", "42", false},
		{"quoted_empty_string", `""`, "", false},
		{"single_quote_pair", `''`, "", false},
		{"unbalanced_double_quote", `"abc`, "\"abc", false},
		{"unbalanced_single_quote", `abc'`, "abc'", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := unquoteValue(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v; wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got=%q; want %q", got, tc.want)
			}
		})
	}
}

// TestUnquoteValueUnescapesDoubleQuote pins the audit fix: a double-quoted value
// with escaped interior quotes (as Save writes them) is unescaped on load, so a
// value containing " survives a save/load round trip.
func TestUnquoteValueUnescapesDoubleQuote(t *testing.T) {
	got, err := unquoteValue(`"hello \"world\""`)
	if err != nil {
		t.Fatalf("unquoteValue: %v", err)
	}
	if got != `hello "world"` {
		t.Errorf("unquoteValue = %q, want %q", got, `hello "world"`)
	}
}
