package adapters

import "testing"

// TestHoistSharedExpressionFieldsBranches pins the early-return and
// per-key mismatch arms of hoistSharedExpressionFields
// (code_risor_parallel.go §9.0.1 hoist rule). The integration test in
// code_risor_parallel_test.go exercises the happy path through the Risor
// script; these table cases cover the guard arms directly:
//
//   - fewer than two results          (399-401)
//   - results[0] missing/empty _expression (404-406)
//   - a candidate value that fails canonicalJSON → field skipped (410-411)
//   - a later result missing its _expression map (416-418)
//   - a later result missing the candidate key entirely (421-424)
func TestHoistSharedExpressionFieldsBranches(t *testing.T) {
	cases := []struct {
		name    string
		results []map[string]any
		wantNil bool
	}{
		{
			name:    "fewer than two results returns nil",
			results: []map[string]any{{"_expression": map[string]any{"a": 1}}},
			wantNil: true,
		},
		{
			name:    "empty results returns nil",
			results: nil,
			wantNil: true,
		},
		{
			name: "first result missing _expression returns nil",
			results: []map[string]any{
				{"no_expr": true},
				{"_expression": map[string]any{"a": 1}},
			},
			wantNil: true,
		},
		{
			name: "first result empty _expression returns nil",
			results: []map[string]any{
				{"_expression": map[string]any{}},
				{"_expression": map[string]any{"a": 1}},
			},
			wantNil: true,
		},
		{
			name: "unmarshalable candidate value is skipped (no panic, no hoist)",
			results: []map[string]any{
				{"_expression": map[string]any{"bad": make(chan int)}},
				{"_expression": map[string]any{"bad": make(chan int)}},
			},
			wantNil: true,
		},
		{
			name: "later result missing _expression map blocks hoist",
			results: []map[string]any{
				{"_expression": map[string]any{"a": 1}},
				{"no_expr": true},
			},
			wantNil: true,
		},
		{
			name: "later result missing candidate key blocks hoist",
			results: []map[string]any{
				{"_expression": map[string]any{"a": 1}},
				{"_expression": map[string]any{"b": 2}},
			},
			wantNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hoistSharedExpressionFields(tc.results)
			if tc.wantNil && got != nil {
				t.Errorf("hoistSharedExpressionFields = %v; want nil", got)
			}
		})
	}
}

// TestHoistSharedExpressionFieldsHappyAndPartial confirms that when a
// field is identical across all results it is hoisted and removed from
// each per-result _expression, while a field that differs stays put.
// This anchors the positive arm (allMatch true → shared[key]=v0) and the
// removal loop alongside the guard cases above.
func TestHoistSharedExpressionFieldsHappyAndPartial(t *testing.T) {
	results := []map[string]any{
		{"_expression": map[string]any{"op_id": "x", "idx": 0.0}},
		{"_expression": map[string]any{"op_id": "x", "idx": 1.0}},
	}
	shared := hoistSharedExpressionFields(results)
	if shared == nil {
		t.Fatal("shared = nil; want op_id hoisted")
	}
	if shared["op_id"] != "x" {
		t.Errorf("shared[op_id] = %v; want x", shared["op_id"])
	}
	if _, ok := shared["idx"]; ok {
		t.Errorf("idx hoisted despite differing across results: %v", shared)
	}
	// op_id must be removed from each per-result _expression.
	for i, r := range results {
		expr := r["_expression"].(map[string]any)
		if _, ok := expr["op_id"]; ok {
			t.Errorf("result[%d] still carries hoisted op_id: %v", i, expr)
		}
		if _, ok := expr["idx"]; !ok {
			t.Errorf("result[%d] lost its non-shared idx: %v", i, expr)
		}
	}
}
