package main

import "testing"

// TestParseUseArgsEmptyUse: `gum schema` walks every command in the tree and
// reads each one's Use line. A command with no Use produced zero fields, and
// the old fields[1:] sliced past the end and panicked, taking the whole
// command down. Cobra permits an empty Use, so this must return no args.
func TestParseUseArgsEmptyUse(t *testing.T) {
	t.Parallel()
	for _, use := range []string{"", "   ", "solo"} {
		got := parseUseArgs(use)
		if len(got) != 0 {
			t.Errorf("parseUseArgs(%q) = %+v; want no args", use, got)
		}
	}
}

// TestParseUseArgsStripsVariadicMarker pins the marker rule. The old
// TrimRight(name, "...") used a cutset, so it ate every trailing dot rather
// than the one literal "..." suffix that marks a variadic argument.
func TestParseUseArgsStripsVariadicMarker(t *testing.T) {
	t.Parallel()
	tests := []struct {
		use  string
		want []string
	}{
		{"call <op-id> [args...]", []string{"op-id", "args"}},
		{"profile test <path>", []string{"path"}},
		{"plugin install <local-dir>", []string{"local-dir"}},
		// A name whose own last character is a dot keeps it: only the literal
		// three-dot marker is a marker.
		{"weird <name.>", []string{"name."}},
		{"weird <name....>", []string{"name."}},
		// A flag-looking token inside brackets is not a positional argument.
		{"cmd [--flag]", nil},
		// Bare words carry no bracket, so they are literals, not arguments.
		{"cmd subcmd <real>", []string{"real"}},
	}
	for _, tc := range tests {
		got := parseUseArgs(tc.use)
		if len(got) != len(tc.want) {
			t.Errorf("parseUseArgs(%q) = %+v; want %v", tc.use, got, tc.want)
			continue
		}
		for i, want := range tc.want {
			if got[i].Name != want {
				t.Errorf("parseUseArgs(%q)[%d] = %q; want %q", tc.use, i, got[i].Name, want)
			}
		}
	}
}
