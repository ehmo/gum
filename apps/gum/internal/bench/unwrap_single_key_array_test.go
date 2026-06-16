package bench

import "testing"

// TestUnwrapSingleKeyArrayBranches pins every branch of the unwrap
// helper: only a single-key map whose sole value is a non-empty []any
// is unwrapped; everything else (non-map, multi-key map, single-key
// map with a non-array or empty-array value) MUST return (nil, false).
//
// Without these cases pinned, a regression to "wrap on any single-key
// map" would silently strip wrappers off responses whose top-level
// scalar/struct shape would then mis-flatten in encodeCompact.
func TestUnwrapSingleKeyArrayBranches(t *testing.T) {
	cases := []struct {
		name    string
		in      any
		wantOK  bool
		wantLen int
	}{
		{
			name: "single_key_with_non_empty_array",
			in: map[string]any{
				"items": []any{1, 2, 3},
			},
			wantOK:  true,
			wantLen: 3,
		},
		{
			name:   "non_map_input",
			in:     []any{1, 2, 3},
			wantOK: false,
		},
		{
			name: "multi_key_map",
			in: map[string]any{
				"items": []any{1},
				"extra": "v",
			},
			wantOK: false,
		},
		{
			name: "single_key_with_scalar_value",
			in: map[string]any{
				"k": "not-an-array",
			},
			wantOK: false,
		},
		{
			name: "single_key_with_empty_array",
			in: map[string]any{
				"items": []any{},
			},
			wantOK: false,
		},
		{
			name:   "nil_input",
			in:     nil,
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			arr, ok := unwrapSingleKeyArray(tc.in)
			if ok != tc.wantOK {
				t.Errorf("ok=%v; want %v", ok, tc.wantOK)
			}
			if tc.wantOK && len(arr) != tc.wantLen {
				t.Errorf("len(arr)=%d; want %d", len(arr), tc.wantLen)
			}
			if !tc.wantOK && arr != nil {
				t.Errorf("arr=%v; want nil on !ok", arr)
			}
		})
	}
}
