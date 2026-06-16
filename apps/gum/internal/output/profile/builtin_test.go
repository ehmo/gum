package profile

import "testing"

// TestBuiltinProfilesValid asserts every embedded built-in profile parses
// cleanly — a malformed built-in (e.g. a multi-line array the line-based parser
// can't read) is an authoring bug that must fail the build, not be silently
// skipped at runtime by loadBuiltins. Parse is the validator for the TOML
// representation; ValidateRawProfileFile targets the JSON representation.
func TestBuiltinProfilesValid(t *testing.T) {
	names := BuiltinNames()
	if len(names) == 0 {
		t.Fatal("no built-in profiles embedded")
	}
	for _, name := range names {
		data, err := builtinFS.ReadFile("builtin/" + name + ".toml")
		if err != nil {
			t.Fatalf("read builtin %s: %v", name, err)
		}
		if _, perr := Parse(string(data)); perr != nil {
			t.Errorf("builtin %s fails to parse: %v", name, perr)
		}
	}
}

func TestBuiltinLookup(t *testing.T) {
	p, ok := BuiltinLookup("googleads.keyword_ideas.v1")
	if !ok || p == nil {
		t.Fatal("expected googleads.keyword_ideas.v1 to resolve")
	}
	if p.Name != "googleads.keyword_ideas.v1" {
		t.Errorf("Name = %q; want googleads.keyword_ideas.v1", p.Name)
	}
	if len(p.KeepFields) == 0 || p.CollapseArrays == nil {
		t.Errorf("profile not parsed as expected: keep=%v collapse=%v", p.KeepFields, p.CollapseArrays)
	}
	if _, ok := BuiltinLookup("nope.does.not.exist"); ok {
		t.Error("unknown profile name must not resolve")
	}
}
