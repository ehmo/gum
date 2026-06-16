package mcp

import "testing"

// TestParseResultsURIAccepts validates the happy path: a clean lowercase-hex
// hash after the gum://results/ prefix parses to the same string.
func TestParseResultsURIAccepts(t *testing.T) {
	t.Parallel()
	cases := []string{
		"gum://results/abcdef0123456789",
		"gum://results/" + repeatHex(64),
		"gum://results/0",
	}
	for _, uri := range cases {
		hash, ok := parseResultsURI(uri)
		if !ok {
			t.Errorf("parseResultsURI(%q) = (_, false); want true", uri)
			continue
		}
		if hash == "" || len(hash) != len(uri)-len(resultsURIPrefix) {
			t.Errorf("parseResultsURI(%q) hash = %q; want trim of prefix", uri, hash)
		}
	}
}

// TestParseResultsURIRejectsMalformed locks down the rejection envelope: any
// non-gum scheme, any embedded path / query / fragment, empty hash, uppercase
// hex, and non-hex chars must all yield (_, false).
func TestParseResultsURIRejectsMalformed(t *testing.T) {
	t.Parallel()
	bad := []string{
		"",                              // empty
		"gum://results/",                // missing hash
		"http://results/abc",            // wrong scheme
		"gum://other/abc",               // wrong host
		"gum://results/abc/def",         // embedded slash
		"gum://results/abc?x=1",         // query
		"gum://results/abc#frag",        // fragment
		"gum://results/ABC123",          // uppercase hex
		"gum://results/abz123",          // non-hex char
		"gum://results/abc.json",        // non-hex char (dot)
		"gum://results/" + repeatHex(63) + "G", // mixed-case sneak-in
	}
	for _, uri := range bad {
		if hash, ok := parseResultsURI(uri); ok {
			t.Errorf("parseResultsURI(%q) = (%q, true); want false", uri, hash)
		}
	}
}

func repeatHex(n int) string {
	out := make([]byte, n)
	const hex = "0123456789abcdef"
	for i := range out {
		out[i] = hex[i%len(hex)]
	}
	return string(out)
}
