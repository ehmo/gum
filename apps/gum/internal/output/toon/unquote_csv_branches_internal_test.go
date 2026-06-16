package toon

import "testing"

// TestUnquoteCSVUnclosedQuoteReturnsUnchanged pins the
// `len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' → return s` guard.
// The function is called from decodeScalar with strings that have an
// opening quote but no closing quote (e.g. a malformed TOON scalar
// like `"foo`); the function MUST surface the input verbatim rather
// than slice off the leading quote — sliceing would mangle a value
// the operator can then diagnose.
func TestUnquoteCSVUnclosedQuoteReturnsUnchanged(t *testing.T) {
	in := `"unclosed`
	if got := unquoteCSV(in); got != in {
		t.Errorf("unquoteCSV(%q)=%q; want unchanged (no trailing quote → bypass strip)", in, got)
	}
}

// TestUnquoteCSVEmptyStringReturnsUnchanged pins the `len(s) < 2`
// branch of the same guard. An empty input string would otherwise
// panic when slicing s[1 : len(s)-1] under the strip path.
func TestUnquoteCSVEmptyStringReturnsUnchanged(t *testing.T) {
	if got := unquoteCSV(""); got != "" {
		t.Errorf("unquoteCSV(\"\")=%q; want \"\"", got)
	}
}
