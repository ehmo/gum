package initpkg

import "testing"

// TestEntryEqualsMapBranches pins the four divergence paths that
// PlanPatch relies on to decide whether an existing mcpServers.gum
// entry already matches what we'd write (no-op) vs. needs a rewrite.
// A regression here would either re-stamp settings.json on every
// `gum init` (churn) or skip a genuinely stale entry (silent drift).
func TestEntryEqualsMapBranches(t *testing.T) {
	want := MCPEntry{Command: "gum", Args: []string{"mcp", "--stdio"}}

	cases := []struct {
		name string
		m    map[string]any
		want bool
	}{
		{
			"exact_match",
			map[string]any{"command": "gum", "args": []any{"mcp", "--stdio"}},
			true,
		},
		{
			"command_mismatch",
			map[string]any{"command": "gummy", "args": []any{"mcp", "--stdio"}},
			false,
		},
		{
			"args_length_mismatch",
			map[string]any{"command": "gum", "args": []any{"mcp"}},
			false,
		},
		{
			"args_value_mismatch",
			map[string]any{"command": "gum", "args": []any{"mcp", "--http"}},
			false,
		},
		{
			"missing_args_treated_as_empty",
			map[string]any{"command": "gum"},
			false,
		},
		{
			"non_string_arg_treated_as_empty",
			map[string]any{"command": "gum", "args": []any{"mcp", 42}},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := entryEqualsMap(want, tc.m); got != tc.want {
				t.Errorf("got=%v; want %v", got, tc.want)
			}
		})
	}
}
