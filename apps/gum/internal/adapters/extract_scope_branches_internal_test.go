package adapters

import "testing"

// TestExtractScopeNonSliceTypeReturnsNil pins the
// `raw not []any → return nil` arm. The dispatch layer hands args
// through MCP / CLI JSON decoding; a malformed caller could pass
// `destructive_scope: "all"` (string) or `destructive_scope: {}`
// (map) instead of an array. The helper MUST return nil so the
// destructive budget enforcement runs on an empty scope rather than
// silently treating a malformed scope as "no restrictions".
func TestExtractScopeNonSliceTypeReturnsNil(t *testing.T) {
	tests := []struct {
		name string
		val  any
	}{
		{"string", "all-the-things"},
		{"map", map[string]any{"op_id": "leaked"}},
		{"number", 42},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractScope(map[string]any{"destructive_scope": tc.val})
			if got != nil {
				t.Errorf("extractScope(%v)=%v; want nil for non-[]any type", tc.val, got)
			}
		})
	}
}

// TestExtractScopeNonMapElementSkipped pins the
// `item not map[string]any → continue` arm. A well-formed slice may
// still contain garbage entries (e.g. a string or number mixed with
// scope maps); the helper MUST skip those rather than panic on the
// type assertion. The contract is "collect what parses, drop what
// doesn't" so a partially-valid scope still constrains the budget.
func TestExtractScopeNonMapElementSkipped(t *testing.T) {
	got := extractScope(map[string]any{
		"destructive_scope": []any{
			"not-a-map",                              // skipped
			42,                                       // skipped
			map[string]any{"op_id": "gum.write"},     // kept
			map[string]any{"resource_key": "rsrc/1"}, // kept
		},
	})
	if len(got) != 2 {
		t.Fatalf("len(got)=%d; want 2 (non-map elements dropped, maps kept)", len(got))
	}
	if got[0].opID != "gum.write" {
		t.Errorf("got[0].opID=%q; want gum.write", got[0].opID)
	}
	if got[1].resourceKey != "rsrc/1" {
		t.Errorf("got[1].resourceKey=%q; want rsrc/1", got[1].resourceKey)
	}
}
